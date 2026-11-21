"""Versioned synthetic Go inputs and source-authored review criteria for S18."""

from hashlib import sha256

from scale_fixture import FANOUT


SOURCE = {
    "routing": {
        "router.go": """package routing

import "errors"

var ErrUnknown = errors.New("unknown route")

type Handler interface { Handle(Request) (Reply, error) }
type Request struct { Route string; Body string }
type Reply struct { Status int; Body string }

type Router struct { handlers map[string]Handler }

func NewRouter(handlers map[string]Handler) Router { return Router{handlers: handlers} }
func (router Router) Dispatch(request Request) (Reply, error) {
	handler, ok := router.handlers[request.Route]
	if !ok { return Reply{}, ErrUnknown }
	return handler.Handle(request)
}
""",
        "handlers.go": """package routing

type Echo struct{}
func (Echo) Handle(request Request) (Reply, error) {
	return Reply{Status: 200, Body: request.Body}, nil
}

type Health struct{}
func (Health) Handle(Request) (Reply, error) { return Reply{Status: 200, Body: "ok"}, nil }

func DefaultRouter() Router {
	return NewRouter(map[string]Handler{"/echo": Echo{}, "/health": Health{}})
}
""",
        "client.go": """package routing

func EchoText(text string) (string, error) {
	reply, err := DefaultRouter().Dispatch(Request{Route: "/echo", Body: text})
	return reply.Body, err
}

func HealthStatus() (int, error) {
	reply, err := DefaultRouter().Dispatch(Request{Route: "/health"})
	return reply.Status, err
}
""",
        "router_test.go": """package routing

import "testing"

func TestRouteContract(t *testing.T) {
	if got, err := EchoText("mail"); got != "mail" || err != nil { t.Fatal(got, err) }
	if got, err := HealthStatus(); got != 200 || err != nil { t.Fatal(got, err) }
	if _, err := DefaultRouter().Dispatch(Request{Route: "/none"}); err != ErrUnknown { t.Fatal(err) }
}
""",
    },
    "billing": {
        "invoice.go": """package billing

type Cents int64
type Line struct { Quantity int64; UnitPrice Cents }
type Invoice struct { Account string; Lines []Line }

func (line Line) Extended() Cents { return Cents(line.Quantity) * line.UnitPrice }
func (invoice Invoice) Subtotal() Cents {
	var sum Cents
	for _, line := range invoice.Lines { sum += line.Extended() }
	return sum
}
""",
        "discount.go": """package billing

type Discount interface { Apply(Cents) Cents }

type Fixed struct { Off Cents }
func (discount Fixed) Apply(subtotal Cents) Cents { return subtotal - discount.Off }

type Percent struct { Rate int64 }
func (discount Percent) Apply(subtotal Cents) Cents { return subtotal * Cents(100 - discount.Rate) / 100 }

func Total(invoice Invoice, discount Discount) Cents { return discount.Apply(invoice.Subtotal()) }
""",
        "receipt.go": """package billing

import "fmt"

const ReceiptPrefix = "receipt"

func Receipt(invoice Invoice, discount Discount) string {
	return fmt.Sprintf("%s:%s:%d", ReceiptPrefix, invoice.Account, Total(invoice, discount))
}
""",
        "receipt_test.go": """package billing

import "testing"

func TestReceiptContract(t *testing.T) {
	invoice := Invoice{Account: "demo", Lines: []Line{{Quantity: 2, UnitPrice: 500}}}
	if got := Receipt(invoice, Fixed{Off: 100}); got != "receipt:demo:900" { t.Fatal(got) }
	if got := Total(invoice, Percent{Rate: 10}); got != 900 { t.Fatal(got) }
}
""",
    },
    "relay": {
        "relay.go": """package relay

const DefaultLabel = "ready"

type Event struct { Code int; Label string }

func Anchor() int { return 7 }
func Current() Event { return Event{Code: Anchor(), Label: DefaultLabel} }
""",
        "consumer.go": """package relay

func ReadCode() int { return Current().Code }
func ReadLabel() string { return Current().Label }
func PublicLabel() string { return ReadLabel() }

func Unrelated() int {
	Anchor := 99
	return Anchor
}
""",
    },
}

MISSING_RELAY = {
    "optional.go": """//go:build bench_missing

package relay

import "example.invalid/missing"

func OptionalCode() int { return missing.Code() }
""",
}

for start in range(0, FANOUT, 128):
    callers = (f"func Caller{i:04d}() int {{ return Anchor() }}" for i in range(start, min(FANOUT, start + 128)))
    SOURCE["relay"][f"fanout{start // 128:02d}.go"] = "package relay\n\n" + "\n".join(callers) + "\n"

DIRTY_RELAY = {
    "relay.go": SOURCE["relay"]["relay.go"].replace('= "ready"', '= "ready" // local operator annotation'),
    "local.go": "package relay\n\nfunc LocalCode() int { return Anchor() }\n",
}

RATIONALE = {
    "routing": "Route dispatch, interface implementations, callers and externally observable constants.",
    "billing": "Value types, integer arithmetic, independent receipt format and interchangeable implementations.",
    "relay": "Shadowed binding, 1025 generated callers, a dirty worktree and an optional unavailable module.",
}

# Criteria are authored from the fixture sources before running MCE4D.
# An @fanout reference expands to every declared caller in its authored file.
CASES = {
    "routing": (
        ("rename", "Rename EchoText to EchoBody and update its uses.", "client.go:EchoText router_test.go:TestRouteContract", "The helper and test use EchoBody; /echo still echoes the exact input."),
        ("interface_change", "Rename Handler.Handle to Serve in the interface and all implementations.", "router.go:Handler router.go:Dispatch handlers.go:Echo handlers.go:Health", "Both implementations conform and Dispatch calls Serve; both routes keep their responses."),
        ("field_change", "Rename Request.Body to Payload without changing the Reply fields.", "router.go:Request router.go:Reply handlers.go:Echo client.go:EchoText", "Request literals and reads change; Reply.Body and echoed content are preserved."),
        ("type_change", "Rename Reply to Response throughout routing.", "router.go:Reply router.go:Handler router.go:Dispatch handlers.go:Echo handlers.go:Health", "Every result and literal uses Response; error and route contracts stay intact."),
        ("behavior_bug", "Accept a nil handler as an unknown route instead of calling it.", "router.go:Dispatch router.go:ErrUnknown", "A map entry with nil yields ErrUnknown and a zero Reply; an existing handler still runs."),
        ("contract_preservation", "Extract the routing strings to named constants and preserve the wire routes.", "handlers.go:DefaultRouter client.go:EchoText client.go:HealthStatus", "Exact route strings /echo and /health remain observable and /none remains unknown."),
        ("behavior_bug", "Make NewRouter snapshot the supplied map so later caller mutations have no effect.", "router.go:NewRouter router.go:Dispatch handlers.go:DefaultRouter", "Adding or replacing a caller map entry after construction cannot change dispatch."),
        ("field_change", "Rename Reply.Status to Code and update consumers.", "router.go:Reply handlers.go:Echo handlers.go:Health client.go:HealthStatus", "Echo and Health still return 200; request shape is untouched."),
        ("contract_preservation", "Extract the health response body into a constant without changing the default answer.", "handlers.go:Health handlers.go:DefaultRouter client.go:HealthStatus", "Health response body remains ok, status remains 200 and /echo is unchanged."),
        ("rename", "Rename DefaultRouter to StandardRouter and migrate both callers and tests.", "handlers.go:DefaultRouter client.go:EchoText client.go:HealthStatus router_test.go:TestRouteContract", "All bound uses update; NewRouter and route/error behavior remain intact."),
    ),
    "billing": (
        ("rename", "Rename the Line.Extended method to Amount and update invoice calculation.", "invoice.go:Extended invoice.go:Subtotal discount.go:Total", "Subtotal continues summing quantity times unit price; receipt amounts are unchanged."),
        ("interface_change", "Rename Discount.Apply to Adjust for both implementations and the caller.", "discount.go:Discount discount.go:Fixed discount.go:Percent discount.go:Total", "Fixed and Percent both implement Adjust and the receipt test still passes."),
        ("field_change", "Rename Line.UnitPrice to UnitCost and update all reads and literals.", "invoice.go:Line invoice.go:Extended receipt_test.go:TestReceiptContract", "Amounts retain integer cents and the receipt test keeps 900."),
        ("type_change", "Rename the Cents type to MinorUnits while preserving its underlying representation.", "invoice.go:Cents invoice.go:Extended discount.go:Discount discount.go:Total", "The type remains int64-backed in every signature and calculation; text format stays exact."),
        ("behavior_bug", "Clamp the fixed discount result to zero when the discount exceeds the amount.", "discount.go:Fixed discount.go:Percent discount.go:Total", "A fixed discount above subtotal returns zero; Percent and valid fixed discounts are unchanged."),
        ("contract_preservation", "Refactor receipt formatting into a private helper while keeping the wire representation.", "receipt.go:Receipt receipt.go:ReceiptPrefix receipt_test.go:TestReceiptContract", "The format remains receipt:<account>:<amount> with decimal cents and no extra whitespace."),
        ("behavior_bug", "Handle nil Discount by returning the unchanged subtotal.", "discount.go:Total invoice.go:Subtotal receipt.go:Receipt", "Nil discount returns the subtotal; Fixed and Percent retain their behavior."),
        ("field_change", "Rename Invoice.Account to AccountID without changing the receipt text.", "invoice.go:Invoice receipt.go:Receipt receipt_test.go:TestReceiptContract", "Keyed literals and reads change; receipt:demo:900 is preserved."),
        ("behavior_bug", "Treat fractional cents in Percent as rounded down for nonnegative amounts and bound Rate above 100.", "discount.go:Percent discount.go:Total receipt_test.go:TestReceiptContract", "For 105 cents at 10 percent return 94; a rate over 100 returns zero; Fixed stays unchanged."),
        ("contract_preservation", "Extract the numeric format into a constant and preserve the public ReceiptPrefix.", "receipt.go:ReceiptPrefix receipt.go:Receipt receipt_test.go:TestReceiptContract", "ReceiptPrefix remains exported and equal to receipt; the complete string stays byte-for-byte identical."),
    ),
    "relay": (
        ("large_fanout", "Rename package-level Anchor to Origin across the repository.", "relay.go:Anchor relay.go:Current consumer.go:Unrelated @fanout", "All 1025 generated callers and Current use Origin. Unrelated's shadowed local Anchor and its value 99 survive."),
        ("dirty_worktree", "Rename package-level Anchor to Origin while preserving existing local changes.", "relay.go:Anchor relay.go:Current local.go:LocalCode consumer.go:Unrelated @fanout", "Preserve the local operator annotation and untracked file. Update its caller as well as the 1025 tracked callers; leave the shadowed binding intact."),
        ("missing_dependency", "Identify why typed context for OptionalCode is incomplete with bench_missing. Report the dependency gap without changing files or downloading modules.", "optional.go:OptionalCode go.mod:example.invalid/missing", "Record that the module is absent from the supplied dependency state, with partial coverage and no suggested unverified edit."),
        ("field_change", "Rename Event.Code to Event.Number and update its declarations and callers.", "relay.go:Event relay.go:Current consumer.go:ReadCode", "ReadCode remains 7; label and direct Anchor callers are unchanged."),
        ("type_change", "Rename Event to Signal without changing its fields.", "relay.go:Event relay.go:Current consumer.go:ReadCode consumer.go:ReadLabel", "Current returns Signal; both observers preserve their values."),
        ("behavior_bug", "Add an Event.IsReady method true only for code 7 and label ready.", "relay.go:Event relay.go:DefaultLabel relay.go:Current", "Current is ready; an event with only a matching code or only a matching label is not."),
        ("contract_preservation", "Extract Current's label initialization into a private helper.", "relay.go:Current relay.go:DefaultLabel consumer.go:ReadLabel", "The observable label is still ready and the numeric code still 7."),
        ("rename", "Rename ReadLabel to DisplayLabel without changing the wire label.", "consumer.go:ReadLabel consumer.go:PublicLabel relay.go:Current", "PublicLabel calls DisplayLabel, which returns ready; numeric and shadowed callers remain untouched."),
        ("dirty_worktree", "Rename Event.Label to StatusText while preserving the existing dirty worktree.", "relay.go:Event relay.go:Current consumer.go:ReadLabel local.go:LocalCode", "Local annotation and untracked LocalCode remain; the default label and caller semantics stay intact."),
        ("large_fanout", "Extract Anchor's value into a private constant and keep every exported function unchanged.", "relay.go:Anchor relay.go:Current consumer.go:Unrelated @fanout", "Every generated caller still returns 7; Current and the shadowed local binding preserve their results."),
    ),
}


def files(repo, variant="clean"):
    mod = f"module example.invalid/bench/{repo}\n\ngo 1.27.4\n"
    if variant == "missing_dependency":
        mod += "\nrequire example.invalid/missing v0.0.1\n"
    contents = {"go.mod": mod, **SOURCE[repo]}
    if variant == "missing_dependency":
        contents.update(MISSING_RELAY)
    if variant == "dirty_worktree":
        contents.update(DIRTY_RELAY)
    return {path: body.encode("utf-8") for path, body in contents.items()}


def fingerprint(contents):
    manifest = "".join(f"{sha256(body).hexdigest()}  {path}\n" for path, body in sorted(contents.items()))
    return sha256(manifest.encode("ascii")).hexdigest()


def tasks():
    for repo, cases in CASES.items():
        clean = files(repo)
        for index, (kind, request, refs, oracle) in enumerate(cases, 1):
            missing = kind == "missing_dependency"
            variant = kind if kind in ("dirty_worktree", "missing_dependency") else "clean"
            start = files(repo, variant)
            required = []
            for ref in refs.split():
                if ref == "@fanout":
                    required.extend(f"fanout{i // 128:02d}.go:Caller{i:04d}" for i in range(FANOUT))
                else:
                    required.append(ref)
            yield {
                "id": f"{repo}-{index:02d}", "repo": repo, "kind": kind,
                "prompt": request + " Work within this checkout and preserve unrelated behavior and files.",
                "required_refs": required, "review_oracle": oracle,
                "verification": "review_only" if missing else "go test -mod=readonly ./...",
                "starting_tree_sha256": fingerprint(clean), "input_sha256": fingerprint(start),
                "files": len(start), "bytes": sum(map(len, start.values())),
                "variant": variant, "dirty": variant != "clean", "build_tags": ["bench_missing"] if missing else [],
                "dependency_state": "synthetic_module_unavailable_offline" if missing else "stdlib_only",
            }
