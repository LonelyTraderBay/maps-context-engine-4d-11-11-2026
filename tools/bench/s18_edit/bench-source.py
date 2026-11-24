import json, sys
from pathlib import Path

ROOT=Path(sys.argv[1]).resolve(strict=True)
paths=json.loads(Path(sys.argv[2]).read_bytes())
files={}
for name in paths:
    try:
        loc=(ROOT/name).resolve(strict=True)
        if loc.is_relative_to(ROOT) and loc.stat().st_size<=750000:
            files[name]=loc.read_text(encoding="utf-8-sig").splitlines()
    except (OSError,UnicodeError):
        continue

def tool(name,description,props,required):
    return dict(name=name,description=description,
        inputSchema=dict(type="object",properties=props,required=required,additionalProperties=False),
        annotations=dict(readOnlyHint=True,destructiveHint=False,openWorldHint=False,idempotentHint=True))
TOOLS=[
    tool("search_source","Search literal text in the tracked source snapshot, with one-based line numbers.",
         {"query":{"type":"string","minLength":1,"maxLength":110},"path_prefix":{"type":"string","maxLength":180}},["query"]),
    tool("read_source","Read numbered lines of one tracked source file. At most 80 lines per call.",
         {"path":{"type":"string"},"start_line":{"type":"integer","minimum":1},"line_count":{"type":"integer","minimum":1,"maximum":80}},
         ["path","start_line","line_count"])
]
def call(name,args):
    if name=="search_source":
        q=args["query"]
        prefix=args.get("path_prefix","")
        if not 1<=len(q)<=110: raise ValueError("keyword length")
        hits=[]
        total=0
        by_file={}
        for path,lines in files.items():
            if path.startswith(prefix):
                for i,line in enumerate(lines,1):
                    if q in line:
                        total+=1
                        by_file[path]=by_file.get(path,0)+line.count(q)
                        if len(hits)<36: hits.append({"path":path,"line":i,"text":line[:700]})
        return {"hits":hits,"total_matching_lines":total,"limit_reached":total>len(hits),"literal_occurrences_by_file":dict(list(by_file.items())[:36]),"file_counts_truncated":len(by_file)>36}
    if name=="read_source":
        path,args_start,count=args["path"],args["start_line"],args["line_count"]
        if path not in files or args_start<1 or not 1<=count<=80: raise ValueError("file or line limit")
        return {"path":path,"lines":[{"line":i,"text":line[:900]}
                 for i,line in enumerate(files[path][args_start-1:args_start-1+count],args_start)]}
    raise ValueError("unknown tool")

for raw in sys.stdin.buffer:
    x=json.loads(raw)
    if "id" not in x: continue
    method=x["method"]
    if method=="initialize":
        result={"protocolVersion":x["params"]["protocolVersion"],"capabilities":{"tools":{}},"serverInfo":{"name":"bench-source","version":"1"}}
    elif method=="tools/list":
        result={"tools":TOOLS}
    elif method=="tools/call":
        try:
            y=call(x["params"]["name"],x["params"]["arguments"])
            result={"content":[{"type":"text","text":json.dumps(y,ensure_ascii=False)}]}
        except (ValueError,KeyError,TypeError):
            result={"content":[{"type":"text","text":"Invalid source query."}],"isError":True}
    else:
        result={}
    print(json.dumps({"jsonrpc":"2.0","id":x["id"],"result":result},ensure_ascii=True),flush=True)