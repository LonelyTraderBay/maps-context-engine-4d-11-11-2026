M="internal/model/"
C="internal/contextpack/"
S="internal/search/"
CASES=[]
def add(id,repo,cat,question,changes,pkg,imports,body,seed=None):
    CASES.append(dict(id=id,repo=repo,category=cat,question=question,mutations=changes,
                      package=pkg,imports=imports,body=body,seed=seed or {}))

add("M01","maps","behavior","Trong bộ đọc JSON định danh, hạ giới hạn độ sâu từ mức hiện tại xuống 24. Chỉ đổi ngưỡng kiểm tra độ lồng và giữ nguyên kiểm tra UTF-8, kích thước cùng thông báo lỗi.",
[(M+"identity_json.go","if depth > 32 {","if depth > 24 {")],M,["strings"],'''
valid:=strings.Repeat("[",24)+"0"+strings.Repeat("]",24)
invalid:="["+valid+"]"
if _,e:=DecodeIdentityJSON([]byte(valid),4096);e!=nil { t.Fatal(e) }
if _,e:=DecodeIdentityJSON([]byte(invalid),4096);e==nil { t.Fatal("accepted depth 25") }
''')
add("M02","maps","contract","Trong JSON định danh, chỉ hạ giới hạn số khóa của một đối tượng xuống 2.048. Vẫn từ chối khóa trùng và vẫn chấp nhận đúng 2.048 khóa khác nhau.",
[(M+"identity_json.go","len(fields) >= 4096","len(fields) >= 2048")],M,["fmt","strings"],'''
var fields []string
for i:=0;i<2049;i++ { fields=append(fields,fmt.Sprintf("%q:0",fmt.Sprint(i))) }
raw:=func(n int)[]byte {return []byte("{"+strings.Join(fields[:n],",")+"}")}
if _,e:=DecodeIdentityJSON(raw(2048),65536);e!=nil {t.Fatal(e)}
if _,e:=DecodeIdentityJSON(raw(2049),65536);e==nil {t.Fatal("accepted 2049")}
if _,e:=DecodeIdentityJSON([]byte("{\\"a\\":1,\\"a\\":2}"),1024);e==nil {t.Fatal("accepted duplicate")}
'''.replace('\\\\','\\'))
add("M03","maps","contract","Khi byte range của chính tham chiếu mã nguồn đã chụp cắt đứt một ký tự UTF-8, chỉ đổi thông báo của tham chiếu đó thành 'source range splits UTF-8'. Giữ nguyên điều kiện kiểm tra, lỗi hash và các thông báo riêng của anchor hay fragment.",
[(M+"source_ref.go",'"range splits UTF-8 encoding"','"source range splits UTF-8"')],M,["strings"],'''
id:="00000000-0000-4000-8000-000000000000"
r:=SourceRef{WorktreeID:id,GenerationID:id,ProfileID:strings.Repeat("a",64),ContentHash:strings.Repeat("b",64),Path:"a.go",Range:ByteRange{StartByte:1,EndByte:2}}
if e:=r.ValidateCapturedBytes([]byte("é")); e==nil || e.Error()!="source range splits UTF-8" {t.Fatalf("%v",e)}
r.Range.StartByte=0
if e:=r.ValidateCapturedBytes([]byte("é"));e==nil || e.Error()!="captured content hash mismatch" {t.Fatalf("%v",e)}
''')
p=M+"source_ref.go"
add("M04","maps","local rename + dirty","Trong kiểm tra đường dẫn của tham chiếu mã nguồn, ở vòng lặp kiểm tra từng thành phần đường dẫn tách bằng dấu gạch chéo, đặt tên biến vòng lặp thành part và sửa các chỗ dùng của nó. Giữ nguyên hành vi và ghi chú đang sửa dở.",
[(p,'for _, segment := range strings.Split(path, "/") {','for _, part := range strings.Split(path, "/") {'),(p,'if segment == "" || segment == "." || segment == ".." {','if part == "" || part == "." || part == ".." {')],M,[],'''
if !validSourcePath("a/b.go") || validSourcePath("a/../b.go") || validSourcePath("a//b.go") {t.Fatal("path rule changed")}
''',{p:"append",})
add("M05","maps","contract","Khi bộ đếm đầu ra MCP báo block rỗng hoặc vượt ngân sách, đổi riêng thông báo lỗi thành 'empty block or delivery budget exceeded'. Giữ số block bắt buộc của chế độ hai bản.",
[(M+"delivery_budget.go",'"delivery exceeds budget or is empty"','"empty block or delivery budget exceeded"')],M,[],'''
b:=Budget{DeliveryMode:"mcp_dual",MaxOutputBytes:4096}
if _,e:=deliveredBytes(b,[][]byte{[]byte("x")});e==nil {t.Fatal("dual accepts one block")}
if _,e:=deliveredBytes(b,[][]byte{[]byte("x"),{}});e==nil || e.Error()!="empty block or delivery budget exceeded" {t.Fatalf("%v",e)}
''')
add("M06","maps","behavior","Đổi TTL dùng chung của cursor phân trang đã ký thành đúng 90 giây. Giữ cấu trúc tính thời điểm hết hạn và kiểm tra lifetime hiện tại.",
[(C+"cursor.go","const cursorTTL = 15 * time.Minute","const cursorTTL = 90 * time.Second")],C,["time"],'''
if cursorTTL!=90*time.Second {t.Fatalf("TTL %v",cursorTTL)}
codec:=CursorCodec{now:func()time.Time{return time.Unix(1000,0)}}
if e:=codec.checkTime(SignedCursor{IssuedAt:999,ExpiresAt:1090});e==nil {t.Fatal("accepted 91-second lifetime")}
if e:=codec.checkTime(SignedCursor{IssuedAt:999,ExpiresAt:1089});e!=nil {t.Fatal(e)}
''')
add("M07","maps","local rename","Trong đường xác minh cursor đã ký, đặt tên biến cục bộ chứa chữ ký vừa giải mã thành providedMAC. Cập nhật các lượt dùng biến đó, giữ nguyên các phép so sánh và kết quả lỗi.",
[(C+"cursor.go","signature, err := base64.RawURLEncoding.DecodeString(parts[1])","providedMAC, err := base64.RawURLEncoding.DecodeString(parts[1])"),(C+"cursor.go","base64.RawURLEncoding.EncodeToString(signature) != parts[1] || !hmac.Equal(signature,","base64.RawURLEncoding.EncodeToString(providedMAC) != parts[1] || !hmac.Equal(providedMAC,")],C,["time"],'''
c:=CursorCodec{now:time.Now}
if _,e:=c.authenticate("invalid","list");e==nil {t.Fatal("accepted invalid cursor")}
''')
add("M08","maps","behavior","Trong bước tính digest của kết quả xem trước đổi tên, hạ số edit tối đa từ 10.000 xuống 4.096. Chỉ đổi điều kiện chặn tổng số edit và giữ nguyên các kiểm tra chồng lấn, tiền điều kiện tệp.",
[(M+"rename_result_digest.go","len(edits) > 10000","len(edits) > 4096")],M,["strings"],'''
_,e:=RenameResultDigest(ScopeVector{},RenameInitialRequest{},strings.Repeat("a",64),[]RenameFilePrecondition{},make([]ProposedEdit,4097),[]Gap{},65536)
if e==nil || e.Error()!="invalid rename result budget" {t.Fatalf("%v",e)}
''')
p=M+"core_data.go"
add("M09","maps","field change + fanout","Trong kiểu kết quả xác thực gói ngữ cảnh, đổi tên trường Go báo tập record đã thay đổi thành RecordsChanged. Giữ nguyên tên khóa JSON và cập nhật mọi nơi dùng trường đó, kể cả file tích hợp sinh sẵn có nhiều caller. Giữ hành vi từ chối xác thực khi record đổi.",
[(p,"RecordSetChanged","RecordsChanged",2),(M+"s18_fanout_fixture.go","RecordSetChanged","RecordsChanged",129)],M,["encoding/json","strings"],'''
b,e:=json.Marshal(ValidateData{RecordsChanged:true})
if e!=nil || !strings.Contains(string(b),`"record_set_changed":true`) || !s18Caller128(ValidateData{RecordsChanged:true}) {t.Fatal(string(b),e)}
''',{M+"s18_fanout_fixture.go":"fanout"})
add("M10","maps","interface contract","Ở cổng interface chỉ đọc dùng cho tra cứu Go, đổi tên chính interface nhận Request và trả Result thành IndexReader. Giữ nguyên phương thức và sửa mọi nơi tham chiếu interface trong bản sao.",
[(S+"search.go","type Reader interface","type IndexReader interface"),(S+"locate_keyset.go","reader Reader","reader IndexReader"),(S+"semantic/index.go","reader search.Reader","reader search.IndexReader")],S,["context"],'''
var _ IndexReader = s18Reader{}
''')
add("U01","uuid","local rename","Trong hàm tạo UUID phiên bản 4 từ luồng io.Reader caller cung cấp, đặt tên tham số luồng thành reader và sửa chỗ đọc đủ byte. Giữ hành vi dựng UUID, bit phiên bản và variant.",
[("version4.go","func NewRandomFromReader(r io.Reader)","func NewRandomFromReader(reader io.Reader)"),("version4.go","io.ReadFull(r, uuid[:])","io.ReadFull(reader, uuid[:])")],"",["bytes"],'''
x,e:=NewRandomFromReader(bytes.NewReader(make([]byte,16)))
if e!=nil || x.Version()!=4 || x.Variant()!=RFC4122 {t.Fatal(x,e)}
''')
add("U02","uuid","behavior","Trong bộ sinh thời gian UUID v7, khi đồng hồ logic không tăng, dùng giá trị trước cộng 2 thay vì cộng 1. Giữ nguyên nhánh tách mili giây và số thứ tự, giữ các bit UUID.",
[("version7.go","now = lastV7time + 1","now = lastV7time + 2")],"",["time"],'''
timeNow=func()time.Time{return time.Unix(0,1000000000)}
lastV7time=0
m,s:=getV7Time(); n,x:=getV7Time()
if (n<<12)+x!=(m<<12)+s+2 {t.Fatal(m,s,n,x)}
''')
add("U03","uuid","parse contract","Ở đường phân tích chuỗi sang giá trị UUID (hàm trả UUID và lỗi), yêu cầu tiền tố URN đúng chữ thường 'urn:uuid:'. Chỉ thay phép so sánh tiền tố trong đường phân tích chuỗi, giữ đường kiểm tra độc lập và các dạng UUID khác.",
[("uuid.go",'if !strings.EqualFold(s[:9], "urn:uuid:") {','if s[:9] != "urn:uuid:" {',1,"func Parse(")],"",[],'''
const x="00000000-0000-0000-0000-000000000000"
if _,e:=Parse("urn:uuid:"+x);e!=nil {t.Fatal(e)}
if _,e:=Parse("URN:UUID:"+x);e==nil {t.Fatal("accepted uppercase")}
if e:=Validate("URN:UUID:"+x);e!=nil {t.Fatal(e)}
''')
add("U04","uuid","local rename","Riêng bộ tạo UUID theo tên phiên bản 5 dùng SHA-1, đặt tên tham số không gian tên thành namespace và cập nhật dòng chuyển tiếp. Giữ nguyên thuật toán, miền và phiên bản 3.",
[("hash.go","func NewSHA1(space UUID","func NewSHA1(namespace UUID"),("hash.go","NewHash(sha1.New(), space, data, 5)","NewHash(sha1.New(), namespace, data, 5)",1,"func NewSHA1(")],"",["crypto/sha1"],'''
x:=NewSHA1(NameSpaceDNS,[]byte("s18"))
if x!=NewHash(sha1.New(),NameSpaceDNS,[]byte("s18"),5) {t.Fatal(x)}
''')
add("U05","uuid","SQL behavior","Khi kiểu UUID thường đọc giá trị SQL nil, đặt lại giá trị UUID về hằng Nil trước khi trả thành công. Chỉ sửa case nil, giữ nhánh UUID có thể null và các dạng dữ liệu khác.",
[("sql.go","case nil:\n\t\treturn nil","case nil:\n\t\t*uuid = Nil\n\t\treturn nil")],"",[],'''
x:=NameSpaceDNS
if e:=x.Scan(nil);e!=nil || x!=Nil {t.Fatal(x,e)}
if e:=x.Scan(NameSpaceDNS.String());e!=nil || x!=NameSpaceDNS {t.Fatal(x,e)}
''')
add("U06","uuid","JSON behavior","Khi kiểu UUID có thể null giải mã JSON null, chỉ đặt cờ hợp lệ về false và giữ 16 byte UUID đang có. Giữ nguyên mọi nhánh đọc JSON khác và kết quả mã hóa null.",
[("null.go","*nu = NullUUID{}","nu.Valid = false",1,"func (nu *NullUUID) UnmarshalJSON(")],"",[],'''
x:=NullUUID{UUID:NameSpaceDNS,Valid:true}
if e:=x.UnmarshalJSON([]byte("null"));e!=nil || x.Valid || x.UUID!=NameSpaceDNS {t.Fatal(x,e)}
s,e:=x.MarshalJSON(); if e!=nil || string(s)!="null" {t.Fatal(string(s),e)}
''')
add("U07","uuid","behavior","Khi sinh timestamp cho UUID v1 và đồng hồ không tiến, tăng clock sequence thêm 2 thay vì 1. Giữ phép mask, bits variant và việc lưu timestamp cuối.",
[("time.go","((clockSeq + 1) & 0x3fff)","((clockSeq + 2) & 0x3fff)")],"",["time"],'''
timeNow=func()time.Time{return time.Unix(0,1000000000)}
clockSeq=0x8004
lasttime=0
_,a,e:=getTime(); if e!=nil {t.Fatal(e)}
_,b,e:=getTime(); if e!=nil || b!=a+2 {t.Fatal(a,b,e)}
''')
add("U08","uuid","input boundary","Hàm đặt định danh nút toàn cục của UUID v1 chỉ chấp nhận đúng 6 byte. Nếu dài hơn hay ngắn hơn đều trả false trước khi chép. Giữ khóa và hành vi với đúng 6 byte.",
[("node.go","if len(id) < 6 {","if len(id) != 6 {")],"",["bytes"],'''
id:=[]byte{1,2,3,4,5,6}
if SetNodeID(append(append([]byte{},id...),7)) || SetNodeID(id[:5]) || !SetNodeID(id) || !bytes.Equal(NodeID(),id) {t.Fatal("length rule")}
''')
add("U09","uuid","error contract","Ở bộ giải mã nhị phân của UUID thường, khi đầu vào không đủ đúng 16 byte đổi thông báo thành 'binary UUID requires 16 bytes (got %d)' và giữ đối số là độ dài thực. Giữ điều kiện, thao tác chép và kiểu nullable.",
[("marshal.go",'"invalid UUID (got %d bytes)"','"binary UUID requires 16 bytes (got %d)"')],"",[],'''
var x UUID
if e:=x.UnmarshalBinary([]byte{1,2});e==nil || e.Error()!="binary UUID requires 16 bytes (got 2)" {t.Fatalf("%v",e)}
if e:=x.UnmarshalBinary(NameSpaceDNS[:]);e!=nil || x!=NameSpaceDNS {t.Fatal(x,e)}
''')
add("U10","uuid","local rename","Ở phương thức trích định danh cục bộ 32-bit big-endian từ UUID DCE, đổi tên receiver thành u và cập nhật dòng đọc. Giữ nguyên phương thức đọc miền bảo mật cạnh đó.",
[("dce.go","func (uuid UUID) ID() uint32 {\n\treturn binary.BigEndian.Uint32(uuid[0:4])","func (u UUID) ID() uint32 {\n\treturn binary.BigEndian.Uint32(u[0:4])")],"",[],'''
x:=UUID{0,0,0,7,0,0,0,0,0,2}
if x.ID()!=7 || x.Domain()!=Domain(2) {t.Fatal(x)}
''')
add("H01","humanize","behavior","Hai lối mặc định định dạng kích thước byte theo SI và IEC đều cần tối thiểu 3 chữ số thay vì 2. Giữ cơ số, bảng đơn vị và các lối cho caller chọn số chữ số.",
[("bytes.go","humanateBytes(s, 1000, 2, sizes)","humanateBytes(s, 1000, 3, sizes)"),("bytes.go","humanateBytes(s, 1024, 2, sizes)","humanateBytes(s, 1024, 3, sizes)")],"",[],'''
for _,n:=range []uint64{82854982,10490,1536} {
 if Bytes(n)!=BytesN(n,3) || IBytes(n)!=IBytesN(n,3) {t.Fatal(n,Bytes(n),IBytes(n))}
}
''')
add("H02","humanize","error contract","Trong đường đọc chuỗi kích thước byte ra uint64, chỉ đổi lỗi đơn vị lạ thành 'unrecognized size unit: %v' với đối số đơn vị hiện có. Giữ nguyên nhánh tràn và đọc số nguyên lớn.",
[("bytes.go",'"unhandled size name: %v"','"unrecognized size unit: %v"')],"",[],'''
if _,e:=ParseBytes("12 zz");e==nil || e.Error()!="unrecognized size unit: zz" {t.Fatalf("%v",e)}
if n,e:=ParseBytes("12 KB");e!=nil || n!=12000 {t.Fatal(n,e)}
''')
add("H03","humanize","behavior","Trong định dạng số thứ tự, đổi hậu tố mặc định từ th sang -th cho bản thử. Giữ các hậu tố st, nd, rd và các điều kiện ngoại lệ 11, 12, 13.",
[("ordinals.go",'suffix := "th"','suffix := "-th"')],"",[],'''
if Ordinal(11)!="11-th" || Ordinal(4)!="4-th" || Ordinal(21)!="21st" {t.Fatal("suffix")}
''')
add("H04","humanize","boundary","Chỉ sửa literal chuỗi đặc biệt của số int64 âm nhỏ nhất khi định dạng có dấu phẩy: dùng dấu trừ Unicode U+2212 thay dấu gạch ASCII ở đầu. Giữ định dạng cho số âm thông thường.",
[("comma.go",'"-9,223,372,036,854,775,808"','"−9,223,372,036,854,775,808"')],"",["math"],'''
if Comma(math.MinInt64)!="−9,223,372,036,854,775,808" || Comma(-1024)!="-1,024" {t.Fatal("negative formatting")}
''')
add("H05","humanize","precision","Trong hàm chuyển số thực thành chuỗi có tham số số chữ số yêu cầu, tăng số chữ số thập phân lúc tạo chuỗi tạm từ sáu lên tám. Giữ cắt chữ số, bỏ số 0 dư và hàm định dạng mặc định.",
[("ftoa.go","FormatFloat(num, 'f', 6, 64), digits","FormatFloat(num, 'f', 8, 64), digits")],"",[],'''
if FtoaWithDigits(1.123456784,8)!="1.12345678" || Ftoa(1.123456784)!="1.123457" {t.Fatal("precision")}
''')
add("H06","humanize","behavior","Khi định dạng thời gian tương đối, chọn ngưỡng đầu tiên lớn hơn hoặc bằng chênh lệch. Chỉ thay phép so sánh trong tìm kiếm nhị phân. Giữ logic nhãn và độ chênh.",
[("times.go","return magnitudes[i].D > diff","return magnitudes[i].D >= diff")],"",["time"],'''
a:=time.Unix(0,0)
m:=[]RelTimeMagnitude{{D:time.Second,Format:"one"},{D:time.Minute,Format:"two"}}
if s:=CustomRelTime(a,a.Add(time.Second),"early","late",m);s!="one" {t.Fatal(s)}
if s:=CustomRelTime(a,a.Add(2*time.Second),"early","late",m);s!="two" {t.Fatal(s)}
''')
add("H07","humanize","floating boundary","Khi tính cặp giá trị và tiền tố SI cho đầu vào bằng 0, trả lại chính giá trị đầu vào thay vì literal 0 để giữ dấu âm của IEEE-754. Giữ tiền tố rỗng và thuật toán cho số khác 0.",
[("si.go",'return 0, ""','return input, ""',1,"func ComputeSI(")],"",["math"],'''
v,p:=ComputeSI(math.Copysign(0,-1))
if !math.Signbit(v) || p!="" {t.Fatal(v,p)}
v,p=ComputeSI(1000)
if v!=1 || p!="k" {t.Fatal(v,p)}
''')
add("H08","humanize","plural contract","Trong lựa chọn từ số nhiều tiếng Anh, dùng dạng số ít cho số lượng 0 cũng như 1. Mở rộng điều kiện hiện có, giữ mọi quy tắc cho số lượng khác, kể cả dạng số nhiều do caller cung cấp.",
[("english/words.go","if quantity == 1 {","if quantity == 0 || quantity == 1 {")],"english/",[],'''
if PluralWord(0,"city","cities")!="city" || PluralWord(1,"city","cities")!="city" || PluralWord(2,"city","cities")!="cities" {t.Fatal("plural")}
''')
add("H09","humanize","format contract","Riêng hàm ghép danh sách kiểu Oxford, trường hợp đúng hai phần tử cũng đặt dấu phẩy trước liên từ. Sửa chuỗi ngăn hai phần tử thành ', %s '. Giữ nguyên đường ghép danh sách thường và các nhánh còn lại.",
[("english/words.go",'strings.Join(words, fmt.Sprintf(" %s ", conjunction))','strings.Join(words, fmt.Sprintf(", %s ", conjunction))',1,"func OxfordWordSeries(")],"english/",[],'''
x:=[]string{"one","two"}
if OxfordWordSeries(x,"and")!="one, and two" || WordSeries(x,"and")!="one and two" {t.Fatal("series")}
''')
add("H10","humanize","missing dependency + contract","Trong bộ đọc dung lượng kiểu số nguyên lớn, chỉ đổi lỗi tên đơn vị lạ thành 'unrecognized big byte unit: %v' với đơn vị hiện tại. Bản sao có file mô phỏng phụ thuộc vắng dưới build tag riêng, giữ nguyên file và không tải phụ thuộc.",
[("bigbytes.go",'"unhandled size name: %v"','"unrecognized big byte unit: %v"')],"",[],'''
if _,e:=ParseBigBytes("10 zz");e==nil || e.Error()!="unrecognized big byte unit: zz" {t.Fatalf("%v",e)}
if n,e:=ParseBigBytes("10 KB");e!=nil || n.String()!="10000" {t.Fatal(n,e)}
''',{"s18_missing_dependency.go":"missing"})