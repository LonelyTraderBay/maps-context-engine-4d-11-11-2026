"""Run the pinned Vietnamese Windows native S18 edit suite."""

import argparse, concurrent.futures as futures, difflib, hashlib, io, json, os, platform, random, re, shutil, subprocess as sp, sys, time, zipfile
from pathlib import Path
from threading import Lock
from edit_cases import CASES

HERE=Path(__file__).resolve().parent
WS=HERE.parents[2]
FIXTURE=HERE/"fixture"
sys.path.insert(0,str(WS/"tools"/"codex_context"))
from main import go_edit_prompt, go_evidence_json, prepare_go_edit_evidence, prepare_prompt, question_request

SOURCES=json.loads((FIXTURE/"sources.json").read_bytes())
CAPTURED=json.loads((FIXTURE/"maps-evidence.json").read_bytes())
GO=shutil.which("go")
FMT=shutil.which("gofmt")
MODEL="gpt-6.2-sol"
SEED=53038
NEGATIVE_CASES={
    "M01","M02","M03","M05","M06","M08","U02","U03","U05","U06","U07","U08",
    "U09","H01","H02","H03","H04","H05","H06","H07","H08","H09","H10",
}
LOCK=Lock()
ENV={k:v for k,v in os.environ.items() if k not in ("OPENAI_API_KEY","CODEX_API_KEY") and not k.startswith("MCE4D_")}
GOENV=dict(ENV,GOTOOLCHAIN="local",GOPROXY="off",GOSUMDB="off",GOENV="off",GOWORK="off",GOFLAGS="",CGO_ENABLED="0",GOMAXPROCS="2")
def dump(path,data): path.write_text(json.dumps(data,indent=2,ensure_ascii=False)+"\n",encoding="utf-8")
def read(path): return json.loads(path.read_bytes())
def sha(data): return hashlib.sha256(data).hexdigest()
def tree(path): return {p.relative_to(path).as_posix():sha(p.read_bytes()) for p in path.rglob("*") if p.is_file()}
def put(path,body):
    path.parent.mkdir(parents=True,exist_ok=True)
    if isinstance(body,str): body=body.encode()
    path.write_bytes(body)

def load_sources():
    for repo, source in SOURCES.items():
        files=source["files"]
        if repo=="maps":
            raw=sp.check_output(["git","-C",WS,"archive","--format=zip",source["upstream"],
                "internal/model","internal/search","internal/contextpack","go.mod","go.sum"],timeout=60)
            with zipfile.ZipFile(io.BytesIO(raw)) as archive:
                contents={path:archive.read(path) for path in files}
        else:
            module=json.loads(sp.check_output([GO,"mod","download","-json",source["module"]],
                cwd=ROOT,env=GOENV,timeout=60))
            if "Error" in module or module["Sum"]!=source["sum"]:
                raise RuntimeError(f"Missing or unverified cached module: {source['module']}")
            cache=Path(module["Dir"])
            contents={path:(cache/path).read_bytes() for path in files}
        if {path:sha(raw) for path,raw in contents.items()}!=files:
            raise RuntimeError(f"Source bytes differ from the locked {repo} snapshot")
        yield repo,contents

def seeded(case,source):
    out=dict(source)
    for path,kind in case["seed"].items():
        if kind=="append":
            out[path]+=b"\n// Local work in progress: preserve this review note.\n"
        elif kind=="fanout":
            code="package model\n\n"
            code+="".join(f"func s18Caller{i:03d}(v ValidateData) bool {{ return v.RecordSetChanged }}\n" for i in range(129))
            out[path]=code.encode()
        elif kind=="missing":
            out[path]=b'//go:build s18_missing_dep\n\npackage humanize\n\nimport _ "s18.invalid/unavailable/fixture"\n'
        else: raise ValueError(kind)
    return out

def reference(case,initial):
    expected=dict(initial)
    for path,old,new,*fields in case["mutations"]:
        count=fields[0] if fields else 1
        text=expected[path].decode()
        after=0
        end=len(text)
        if len(fields)>1:
            after=text.index(fields[1])
            next_function=text.find("\nfunc ",after+1)
            end=next_function if next_function>=0 else len(text)
        assert text[after:end].count(old)==count,(case["id"],path,old,text[after:end].count(old))
        expected[path]=(text[:after]+text[after:end].replace(old,new)+text[end:]).encode()
    return expected

def fixture_test(case):
    package=case["package"].rstrip("/").split("/")[-1] or ("uuid" if case["repo"]=="uuid" else "humanize")
    imports="\n".join(json.dumps(m) for m in ["testing",*case["imports"]])
    outside=""
    if case["id"]=="M10":
        outside="type s18Reader struct{}\nfunc(s18Reader) Search(context.Context,Request)(Result,error){return Result{},nil}\n"
    return f"package {package}\n\nimport (\n{imports}\n)\n\n{outside}\nfunc TestExternalS18(t *testing.T) {{\n{case['body']}\n}}\n"

def prepare_base():
    base=ROOT/"verification"
    marker=base/"ready.json"
    if marker.exists(): return
    # Include the committed Go dependency closure for Maps' isolated fixture.
    rev=SOURCES["maps"]["upstream"]
    raw=sp.check_output(["git","-C",str(WS),"archive","--format=zip",rev,"internal","contracts","go.mod","go.sum"],timeout=60)
    with zipfile.ZipFile(io.BytesIO(raw)) as z:
        for name in z.namelist():
            if name.endswith("/") or name.endswith("_test.go"): continue
            if Path(name).suffix not in (".go",".json",".mod",".sum",".sql",".txt",".s"): continue
            assert ".." not in Path(name).parts
            put(base/"maps"/name,z.read(name))
    for repo in ("uuid","humanize"):
        for path,data in BASE[repo].items():
            if path.endswith("_test.go"): continue
            if path.endswith((".go",".mod",".sum")):
                put(base/repo/path,data)
    assert not any(base.rglob("*_test.go"))
    dump(marker,{"maps_revision":rev,"source":"pinned production code only; no original tests"})

def verify(case,patch,folder):
    folder.mkdir(parents=True,exist_ok=True)
    base=ROOT/"verification"/case["repo"]
    mapping={}
    for path,raw in patch.items():
        file=folder/"overlay"/path
        put(file,raw)
        mapping[str(base/path)]=str(file)
    test=folder/"private_test.go"
    put(test,fixture_test(case))
    mapping[str(base/case["package"]/"zz_s18_private_test.go")]=str(test)
    overlay=folder/"overlay.json"
    dump(overlay,{"Replace":mapping})
    pkg="./"+case["package"].rstrip("/") if case["package"] else "."
    if case["id"]=="M10": pkg="./internal/search/..."
    try:
        p=sp.run([GO,"test","-mod=readonly","-vet=off","-run","^TestExternalS18$","-count=1","-overlay",overlay,pkg],
                 cwd=base,env=GOENV,capture_output=True,timeout=80,creationflags=sp.CREATE_NO_WINDOW)
        (folder/"go.stdout").write_bytes(p.stdout)
        (folder/"go.stderr").write_bytes(p.stderr)
        return p.returncode==0 and b"ok " in p.stdout.replace(b"\t",b" ")
    except sp.TimeoutExpired: return False

def plan():
    global BASE
    BASE=dict(load_sources())
    rows=[]
    for case in CASES:
        initial=seeded(case,BASE[case["repo"]])
        expected=reference(case,initial)
        target=[p for p in initial if initial[p]!=expected[p]]
        assert target
        rows.append(dict(case=case,baseline_files={p:sha(v) for p,v in initial.items()},
                         expected_changed_files={p:sha(expected[p]) for p in target},
                         verifier_sha256=sha(fixture_test(case).encode())))
    assert len(rows)==30
    frozen=dict(platform="windows/amd64",model=MODEL,seed=SEED,cases=rows,
                source_lock_sha256=sha((FIXTURE/"sources.json").read_bytes()),
                captured_excerpts_sha256=sha((FIXTURE/"maps-evidence.json").read_bytes()),
                host_adapter_sha256={name:sha((WS/"tools"/"codex_context"/name).read_bytes())
                                     for name in ("main.py","edit_evidence.py")})
    dest=ROOT/"frozen-edit-plan.json"
    if dest.exists(): assert read(dest)==json.loads(json.dumps(frozen))
    else: dump(dest,frozen)
    return rows

def vet_reference(row):
    case=row["case"]
    initial=seeded(case,BASE[case["repo"]])
    gold=reference(case,initial)
    targets=set(row["expected_changed_files"])|set(case["seed"])
    return case["id"],verify(case,{p:gold[p] for p in targets},ROOT/"reference-validation"/case["id"])

def vet_negative(case):
    original=seeded(case,BASE[case["repo"]])
    folder=ROOT/"negative-controls"/case["id"]
    passed=verify(case,{path:original[path] for path in case["seed"]},folder)
    stdout=folder/"go.stdout"
    return case["id"],not passed and stdout.is_file() and b"--- FAIL: TestExternalS18" in stdout.read_bytes()

def make_prompt(question,packet,initial,baseline):
    excerpts=[]
    for item in packet["data"].get("excerpts",[]):
        ref=item["source_ref"]
        path=ref["path"]
        data=initial.get(path)
        captured=baseline.get(path)
        if data is None or captured is None or sha(captured)!=ref["content_hash"]:
            continue
        # A fixture may append an existing edit beyond the indexed bytes.
        # Reuse only exact captured prefixes and verify the full excerpt.
        if not data.startswith(captured):
            continue
        a,b=ref["range"]["start_byte"],ref["range"]["end_byte"]
        if not 0<=a<b<=len(captured) or data[a:b].decode()!=item["content"] or data[:a].count(b"\n")+1!=item["start_line"]:
            raise RuntimeError("prefetch source does not match task checkout")
        excerpts.append(item)
    focused=prepare_go_edit_evidence(question,excerpts,initial)
    if focused is not None:
        return go_edit_prompt(question,focused),1
    evidence=go_evidence_json(excerpts)
    count=len(json.loads(evidence))
    if not count: return question,0
    return question+"\n\nĐoạn mã xếp theo mức liên quan và đã đối chiếu với bản sao; lines là [số dòng, văn bản]. Ưu tiên dùng trực tiếp đoạn đáp ứng đúng yêu cầu. Đây là ứng viên trích từ nguồn.\n"+evidence,count


def canonical(data):
    p=sp.run([FMT],input=data,capture_output=True,timeout=8)
    if p.returncode: return None
    return p.stdout

def edits_in(before,after):
    matcher=difflib.SequenceMatcher(a=before.decode().splitlines(),b=after.decode().splitlines(),autojunk=False)
    return [(i,j) for op,i,j,k,l in matcher.get_opcodes() if op!="equal"]

def overlapping(a,b):
    x,y=a; u,v=b
    if x==y or u==v:
        return x<=v and u<=y
    return x<v and u<y

def run(entry):
    row,arm=entry
    case=row["case"]
    slug,id=case["repo"],case["id"]
    folder=ROOT/"runs"/f"{id}-{arm}"
    result=folder/"measurement.json"
    if result.exists(): return read(result)
    clone=folder/"checkout"
    clone.mkdir(parents=True,exist_ok=False)
    initial=seeded(case,BASE[slug])
    for path,body in initial.items(): put(clone/path,body)
    assert tree(clone)==row["baseline_files"]
    dump(folder/"visible-files.json",sorted(initial))
    question=question_for(case)
    context=question
    retrieval=None
    if arm=="C":
        with LOCK:
            tick=time.perf_counter()
            try:
                packet=get_packet(case,question)
                context,count=make_prompt(question,packet,initial,BASE[slug])
                dump(folder/"maps.json",packet)
                retrieval=dict(ok=True,excerpts=count)
                supplied=json.loads(context.split("\n")[-1]) if count else None
                if isinstance(supplied,dict):
                    dump(folder/"edit-evidence.json",supplied)
                    retrieval["edit_evidence_kind"]=supplied["kind"]
            except (RuntimeError,ValueError,OSError,sp.TimeoutExpired) as e:
                retrieval=dict(ok=False,reason=str(e))
            retrieval.update(input_tokens=0,ms=round((time.perf_counter()-tick)*1000))
    fields={"path":{"type":"string"},"old":{"type":"string"},"new":{"type":"string"},"occurrences":{"type":"integer","minimum":1}}
    schema={"type":"object","properties":{"edits":{"type":"array","items":{"type":"object","properties":fields,
            "required":list(fields),"additionalProperties":False},"maxItems":12}},"required":["edits"],"additionalProperties":False}
    dump(folder/"schema.json",schema)
    banner=("Sửa mã theo yêu cầu bên dưới bằng đề xuất JSON. Bạn có bản sao Go chỉ đọc; harness sẽ áp dụng. "
        "Cấm chạy shell, script, mã nguồn, build hoặc test repo đích. Chỉ dùng search_source và read_source khi cần. "
        "Nếu đoạn mã kèm theo đã đủ, dùng trực tiếp. Trả các phép thay thế theo thứ tự, mỗi mục có path tương đối, old và new là chuỗi mã nguồn nguyên văn, và occurrences là số lần old phải xuất hiện. "
        "Giữ tab/xuống dòng của old. Dùng ngữ cảnh để tránh thay các chỗ khác. Có thể thay lặp trong một tệp nếu mọi lần đều được yêu cầu. "
        "Chỉ sửa câu lệnh và khai báo cần thiết. Giữ nguyên comment, ví dụ và test của bản sao. Khi đã có nguyên văn và đủ phạm vi, áp dụng trực tiếp. Nếu đổi tên ở mọi chỗ dùng, ưu tiên một truy vấn toàn bản sao bằng định danh cũ, và dựa vào thống kê toàn bộ theo tệp. Khi còn thiếu dữ kiện, đọc thêm bằng công cụ đọc nguồn. ")
    args=[CODEX,"exec","--ignore-user-config","--ephemeral","--json","--sandbox","read-only","--skip-git-repo-check",
          "-c",'approval_policy="never"',"-c",'model_reasoning_effort="low"',"-m",MODEL,"-C",str(clone),"--output-schema",str(folder/"schema.json"),
          "-c","features.shell_tool=false","-c",'web_search="disabled"',
          "-c","mcp_servers.bench_source={command="+json.dumps(sys.executable)+",args="+json.dumps([str(HERE/"bench-source.py"),str(clone),str(folder/"visible-files.json")])+
          ',required=true,default_tools_approval_mode="approve",startup_timeout_sec=20}']
    tick=time.perf_counter()
    with (folder/"events.jsonl").open("wb") as out,(folder/"stderr.log").open("wb") as err:
        try:
            code=sp.run([*args,"-"],input=(banner+context).encode(),
                cwd=clone,stdout=out,stderr=err,env=ENV,timeout=140,creationflags=sp.CREATE_NO_WINDOW).returncode
        except sp.TimeoutExpired:code=None
    ms=round((time.perf_counter()-tick)*1000)+(retrieval["ms"] if retrieval else 0)
    assert tree(clone)==row["baseline_files"],"model modified read-only clone"
    events=[]
    for line in (folder/"events.jsonl").read_bytes().splitlines():
        try:events.append(json.loads(line))
        except (ValueError,UnicodeError):pass
    items=[e["item"] for e in events if e.get("type")=="item.completed"]
    usage=[e["usage"] for e in events if e.get("type")=="turn.completed" and isinstance(e.get("usage"),dict)]
    shell=[e.get("command") for e in items if e.get("type")=="command_execution"]
    calls=[e.get("tool") for e in items if e.get("type")=="mcp_tool_call"]
    messages=[e["text"] for e in items if e.get("type")=="agent_message"]
    try: actions=json.loads(messages[-1])["edits"]
    except (ValueError,IndexError,KeyError,TypeError):actions=[]
    dump(folder/"proposal.json",actions)
    actual=dict(initial)
    wrong=[]
    invalid=[]
    scope=set(row["expected_changed_files"])
    if not isinstance(actions,list) or len(actions)>12:
        invalid.append("bad edit list")
        actions=[]
    for a in actions:
        try:
            path=a["path"].replace("\\","/")
            old,new=a["old"].encode(),a["new"].encode()
            n=a["occurrences"]
            if path not in initial or not path.endswith(".go"):
                wrong.append(path)
                continue
            if not old or len(old)>15000 or len(new)>15000 or type(n)!=int or n<1 or actual[path].count(old)!=n:
                invalid.append(path)
                continue
            actual[path]=actual[path].replace(old,new)
        except (KeyError,AttributeError,TypeError) as e:
            invalid.append(type(e).__name__)
    changed={p for p in initial if initial[p]!=actual[p]}
    expected=reference(case,initial)
    for p in sorted(changed):
        put(clone/p,actual[p])
        if p not in scope:
            wrong.append(p)
        else:
            spans=edits_in(initial[p],expected[p])
            for edit in edits_in(initial[p],actual[p]):
                if not any(overlapping(edit,allowed) for allowed in spans):
                    wrong.append(f"{p}:{edit[0]+1}")
    verified_tree=tree(clone)
    assert verified_tree=={p:sha(v) for p,v in actual.items()}
    exact=changed==scope and all(canonical(actual[p]) is not None and canonical(actual[p])==canonical(expected[p]) for p in scope)
    compile_status="not_run"
    if code==0 and exact and not wrong and not invalid and not shell:
        # Execute only reference-equivalent Go and our own private test.
        compile_status="pass" if verify(case,{p:actual[p] for p in scope|set(case["seed"])},
                                       folder/"verification") else "fail"
    rec=dict(case=id,repo=slug,arm=arm,category=case["category"],
             outcome="accepted" if compile_status=="pass" else "timeout" if code is None else "failed",
             usage=usage[-1] if usage else None,elapsed_ms=ms,discovery=retrieval,mcp_calls=calls,
             shell_count=len(shell),false_edits=sorted(set(wrong)),invalid_edits=invalid,changed=sorted(changed),
             expected=sorted(scope),exact=exact,fixture=compile_status,exit=code,
             host_errors=[e.get("message","") for e in events if e.get("type")=="error"])
    dump(result,rec)
    return rec

def question_for(case):
    question=case["question"]
    if case["seed"]:
        question+=" Bảo toàn các thay đổi có sẵn nằm ngoài yêu cầu."
    return question


def get_packet(case, question):
    if MAPS is None:
        return CAPTURED[case["id"]]
    scope=BINDINGS[case["repo"]]
    _,packet=prepare_prompt(question,MAPS,scope["worktree"],scope["profile"],
        env=dict(ENV,MCE4D_HOME=scope["profile_home"],MCE4D_WINDOWS_ROLE_IPC="1",GOMAXPROCS="4"))
    check_live_scope(packet,scope)
    return packet


def check_live_scope(packet, scope):
    members=(packet.get("scope") or {}).get("members",[])
    expected=(scope["worktree"],scope["profile"],scope["generation"])
    if packet.get("error") or len(members)!=1:
        raise RuntimeError("Maps did not return one authorized snapshot")
    def identity(ref):
        return ref["worktree_id"],ref["profile_id"],ref["generation_id"]
    if identity(members[0])!=expected:
        raise RuntimeError("Maps returned a different scope from the live binding")
    if not any(gap.get("code")=="GO_QUESTION_CANDIDATES" for gap in packet["coverage"]["gaps"]):
        raise RuntimeError("Maps build does not provide Go question excerpts")
    for excerpt in (packet.get("data") or {}).get("excerpts",[]):
        if identity(excerpt["source_ref"])!=expected:
            raise RuntimeError("Maps returned a Go excerpt from a different scope")


def check_live_capture(capture):
    expected=read(FIXTURE/"prompt-hashes.json")
    scopes=capture["scopes"]
    results=capture["results"]
    if capture["maps_index_bytes"]<=4<<20:
        raise RuntimeError("The live Maps fixture did not exercise an index above 4 MiB")
    if set(scopes)!=set(SOURCES) or set(results)!={case["id"] for case in CASES}:
        raise RuntimeError("Live capture must contain all 30 cases from the three prepared repositories")
    actual={}
    for case in CASES:
        case_id=case["id"]
        packet=results[case_id]
        check_live_scope(packet,scopes[case["repo"]])
        source=BASE[case["repo"]]
        prompt,count=make_prompt(question_for(case),packet,seeded(case,source),source)
        actual[case_id]={"sha256":sha(prompt.encode()),"sections":count}
        if not count or actual[case_id]!=expected[case_id]:
            raise RuntimeError(f"Live C prompt differs from the accepted run: {case_id}")
    dump(ROOT/"live-prompt-check.json",{"maps_index_bytes":capture["maps_index_bytes"],"prompts":actual})
    print(f"Opened Maps index: {capture['maps_index_bytes']} bytes (> 4 MiB); {len(actual)}/30 live C prompts match byte for byte")


def replay_check():
    expected=read(FIXTURE/"prompt-hashes.json")
    actual={}
    for case in CASES:
        source=BASE[case["repo"]]
        question=question_for(case)
        prompt,count=make_prompt(question,CAPTURED[case["id"]],seeded(case,source),source)
        actual[case["id"]]={"sha256":sha(prompt.encode()),"sections":count}
    if actual!=expected:
        raise RuntimeError("Replayed C prompts differ from the accepted run")
    dump(ROOT/"prompt-replay.json",actual)
    print(f"{len(actual)}/30 C prompts match the accepted run byte for byte")


def main():
    global ROOT,CODEX,MAPS,BINDINGS
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command",choices=("prepare","preflight","replay-check","live-check","run"))
    parser.add_argument("--output",type=Path,required=True)
    parser.add_argument("--codex",type=Path)
    parser.add_argument("--maps",type=Path)
    parser.add_argument("--bindings",type=Path)
    parser.add_argument("--capture",type=Path,help="CLI responses written by the packaged Windows fixture")
    parser.add_argument("--jobs",type=int,default=7,choices=range(1,17),metavar="1–16")
    args=parser.parse_args()
    if sys.platform!="win32" or platform.machine().lower() not in ("amd64","x86_64"):
        parser.error("this suite requires Windows native amd64")
    if not GO or not FMT:
        parser.error("go.exe and gofmt.exe must be available on PATH")
    ROOT=args.output.resolve()
    if args.command=="prepare":
        ROOT.mkdir(parents=True,exist_ok=False)
        dump(ROOT/"suite.json",{"fixture_sha256":sha((FIXTURE/"sources.json").read_bytes())})
    else:
        if read(ROOT/"suite.json")!={"fixture_sha256":sha((FIXTURE/"sources.json").read_bytes())}:
            parser.error("output was not prepared for this fixture")
    rows=plan()
    if args.command=="prepare":
        for repo,files in BASE.items():
            for path,raw in files.items():
                put(ROOT/"source"/repo/path,raw)
        dump(ROOT/"live-requests.json",[
            {"case":case["id"],"repo":case["repo"],"input":question_request(question_for(case))}
            for case in CASES
        ])
        return 0
    if args.command=="replay-check":
        replay_check()
        return 0
    if args.command=="live-check":
        if not args.capture:
            parser.error("live-check needs --capture from the packaged Windows fixture")
        check_live_capture(read(args.capture))
        return 0
    prepare_base()
    if args.command=="preflight":
        with futures.ThreadPoolExecutor(max_workers=min(args.jobs,6)) as pool:
            results=dict(pool.map(vet_reference,rows))
            controls=dict(pool.map(vet_negative,(case for case in CASES if case["id"] in NEGATIVE_CASES)))
        dump(ROOT/"reference-results.json",results)
        dump(ROOT/"negative-controls.json",controls)
        print(json.dumps({"reference":results,"negative_controls":controls}),flush=True)
        return 0 if all(results.values()) and all(controls.values()) else 1

    if not args.codex or bool(args.maps)!=bool(args.bindings):
        parser.error("run needs --codex. Live mode also needs both --maps and --bindings")
    CODEX=args.codex.resolve(strict=True)
    MAPS=args.maps.resolve(strict=True) if args.maps else None
    BINDINGS=read(args.bindings) if args.bindings else {}
    references=read(ROOT/"reference-results.json")
    controls=read(ROOT/"negative-controls.json")
    if set(references)!={case["id"] for case in CASES} or set(controls)!=NEGATIVE_CASES \
            or not all(references.values()) or not all(controls.values()):
        raise RuntimeError("preflight did not pass")
    config={"mode":"live" if MAPS else "replay","model":MODEL,"seed":SEED,
            "maps_sha256":sha(MAPS.read_bytes()) if MAPS else None,"bindings":BINDINGS}
    config_file=ROOT/"run-config.json"
    if config_file.exists():
        raise RuntimeError("use a fresh output directory for each run")
    # Verify all C inputs before opening the first model turn.
    if not MAPS:
        replay_check()
    else:
        if set(BINDINGS)!=set(SOURCES):
            parser.error("bindings must name maps, uuid and humanize")
        for case in CASES:
            question=question_for(case)
            source=BASE[case["repo"]]
            packet=get_packet(case,question)
            if make_prompt(question,packet,seeded(case,source),source)[1]==0:
                raise RuntimeError(f"Live scope has no matching excerpts for {case['id']}")
    dump(config_file,config)
    work=[(row,arm) for row in rows for arm in "AC"]
    random.Random(SEED).shuffle(work)
    with futures.ThreadPoolExecutor(max_workers=args.jobs) as pool:
        for run_result in futures.as_completed(pool.submit(run,item) for item in work):
            print(json.dumps(run_result.result(),ensure_ascii=True),flush=True)
    return 0


if __name__=="__main__":
    sys.exit(main())
