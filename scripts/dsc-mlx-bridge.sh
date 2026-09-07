#!/bin/bash
# DSC→本地MLX桥 v3（只读工具版）
# 安全设计（2026-09-08 事故后重写）：
#   v2 的 run_shell 黑名单被模型主动绕过（正则只拦 rm -rf，模型换写法清空了 ~/models）。
#   v3 彻底移除任意命令执行，只提供白名单化的只读专用工具，
#   全部用 Python 标准库实现，不给模型任何 shell。
[ "$1" = "--profile" ] && shift 2
P="$*"
python3 - "$P" <<'PY'
import json, os, re, sys, urllib.request

MODEL_PATH = "/Users/fuigui/models/huihui-qwen35-9b-abl"
API = "http://127.0.0.1:8080/v1/chat/completions"
MAX_ROUNDS = 4
LIST_CAP, READ_CAP = 40, 3000   # 目录条目上限 / 读文件字符上限

def parse_prompt(blob):
    sysmsg = ("你是运行在老板 Mac 上的微信助手，可查询这台机器的文件与磁盘信息（只读）。"
              "你不能修改、删除、新建任何文件；不要承诺这类操作，被问到就说明自己只有只读查询能力。")
    hist, cur = [], blob
    m = re.search(r"以下是之前的对话记录（供上下文参考）：\n(.*?)\n\n用户最新消息：", blob, re.S)
    if m:
        cur = blob[m.end():]
        for blk in m.group(1).split("\n---\n"):
            blk = blk.strip()
            if blk.startswith("**用户**："):
                hist.append({"role": "user", "content": blk[len("**用户**："):].strip()})
            elif blk.startswith("**DSC**："):
                hist.append({"role": "assistant", "content": blk[len("**DSC**："):].strip()})
    tail = re.search(r"\n\n请遵守微信回复规范：(.*)$", cur, re.S)
    if tail:
        sysmsg += "\n请遵守微信回复规范：" + tail.group(1).strip()
        cur = cur[:tail.start()]
    return ([{"role": "system", "content": sysmsg}] + hist
            + [{"role": "user", "content": cur.strip()}])

TOOLS = [
    {"type": "function", "function": {
        "name": "list_dir",
        "description": "列出目录的条目（名称、大小、是否目录）。只读。",
        "parameters": {"type": "object", "properties": {
            "path": {"type": "string"}}, "required": ["path"]}}},
    {"type": "function", "function": {
        "name": "read_file",
        "description": "读取一个文本文件的内容（截断）。只读。",
        "parameters": {"type": "object", "properties": {
            "path": {"type": "string"}, "max_chars": {"type": "integer", "default": READ_CAP}},
            "required": ["path"]}}},
    {"type": "function", "function": {
        "name": "dir_size",
        "description": "统计一个目录的总大小（MB）与一级子项。只读。",
        "parameters": {"type": "object", "properties": {
            "path": {"type": "string"}}, "required": ["path"]}}},
]

def expand(p):
    return os.path.abspath(os.path.expanduser(p or "."))

def exec_tool(name, args):
    try:
        path = expand(args.get("path", "."))
        if name == "list_dir":
            ents = sorted(os.listdir(path))[:LIST_CAP]
            rows = []
            for e in ents:
                fp = os.path.join(path, e)
                if os.path.islink(fp):
                    rows.append(f"{e} -> {os.readlink(fp)}")
                elif os.path.isdir(fp):
                    rows.append(f"{e}/")
                else:
                    rows.append(f"{e}  {os.path.getsize(fp)//1024}KB")
            return "\n".join(rows) or "(空目录)"
        if name == "read_file":
            with open(path, errors="replace") as f:
                return f.read(min(int(args.get("max_chars", READ_CAP)), READ_CAP))
        if name == "dir_size":
            total = 0
            for root, _, files in os.walk(path):
                for fn in files:
                    try: total += os.path.getsize(os.path.join(root, fn))
                    except OSError: pass
            subs = []
            for e in sorted(os.listdir(path))[:LIST_CAP]:
                fp = os.path.join(path, e)
                if os.path.isdir(fp):
                    st = 0
                    for root, _, files in os.walk(fp):
                        for fn in files:
                            try: st += os.path.getsize(os.path.join(root, fn))
                            except OSError: pass
                    subs.append(f"{e}/  {st//1048576}MB")
            return f"总计 {total//1048576}MB\n" + "\n".join(subs)
        return f"未知工具: {name}"
    except FileNotFoundError:
        return "路径不存在。"
    except Exception as e:
        return f"查询出错: {type(e).__name__}: {e}"

def chat(messages, use_tools):
    body = {"model": MODEL_PATH, "messages": messages, "max_tokens": 1500,
            "chat_template_kwargs": {"enable_thinking": False}}
    if use_tools:
        body["tools"] = TOOLS
    req = urllib.request.Request(API, data=json.dumps(body).encode(),
                                 headers={"Content-Type": "application/json"})
    return json.load(urllib.request.urlopen(req, timeout=900))["choices"][0]["message"]

def main():
    messages = parse_prompt(sys.argv[1])
    for round_ in range(MAX_ROUNDS + 1):
        msg = chat(messages, use_tools=(round_ < MAX_ROUNDS))
        calls = msg.get("tool_calls") or []
        if not calls:
            out = (msg.get("content") or "").strip()
            if not out:
                r = msg.get("reasoning") or ""
                out = r[-600:] if r else "（模型未返回内容）"
            print(out)
            return
        messages.append({"role": "assistant", "content": msg.get("content") or "",
                         "tool_calls": calls})
        for c in calls:
            fn = c["function"]
            try:
                args = json.loads(fn.get("arguments") or "{}")
            except json.JSONDecodeError:
                args = {}
            messages.append({"role": "tool", "tool_call_id": c.get("id", fn.get("name")),
                             "content": str(exec_tool(fn.get("name", ""), args))[:2500]})
    print("（工具调用轮数超限，已停止。请换个更直接的问法。）")

main()
PY
