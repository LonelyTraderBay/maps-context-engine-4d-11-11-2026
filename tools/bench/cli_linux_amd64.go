package bench

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"time"

	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/analysis/providers"
	"github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/internal/app"
	pythonprovider "github.com/LonelyTraderBay/maps-context-engine-4d-11-11-2026/packages/python-provider"
)

// The shipped harness is trusted code. The suite is supplied only on stdin.
//
//go:embed scale_fixture.py
var scaleFixture string

//go:embed corpus.py
var corpus string

//go:embed report.py
var report string

//go:embed main.py
var mainModule string

func RenderSuite(ctx context.Context, suite []byte) ([]byte, error) {
	const stdlib = "/usr/lib/python3.14"
	pin, err := pythonprovider.PinnedToolchain()
	if err != nil {
		return nil, err
	}
	python, err := providers.OpenPinnedPythonExecutable("/usr/bin/python3.14", pin.Interpreter)
	if err != nil {
		return nil, app.ErrAdminUnavailable
	}
	defer python.Close()
	if digest, err := providers.PythonStdlibDigest(ctx, stdlib); err != nil || digest != pin.Stdlib {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, app.ErrAdminUnavailable
	}
	modules, err := json.Marshal([]string{scaleFixture, corpus, report, mainModule})
	if err != nil {
		return nil, err
	}
	program := `
import sys
if sys.path != ['/usr/lib/python314.zip', '/usr/lib/python3.14', '/usr/lib/python3.14/lib-dynload']:
    sys.exit(3)
sys.path.pop(0)
import json
from types import ModuleType
for name, source in zip(('scale_fixture', 'corpus', 'report', '_mce4d_bench'), ` + string(modules) + `):
    module = ModuleType(name)
    sys.modules[name] = module
    exec(compile(source, name, 'exec'), module.__dict__)
try:
    suite = json.loads(sys.stdin.buffer.read().decode('utf-8'), object_pairs_hook=module.unique_object)
    if type(suite) is not dict or set(suite) != {'schema_version', 'plan', 'observations'} or type(suite['schema_version']) is not int or suite['schema_version'] != 1:
        sys.exit(2)
    report = module.render_report(module.verify_plan(suite['plan']), suite['observations'])
    print(json.dumps(report, sort_keys=True, ensure_ascii=True, allow_nan=False, indent=2))
except (ValueError, TypeError, OverflowError, RecursionError):
    sys.exit(2)
`
	cmd := exec.CommandContext(ctx, "/proc/self/fd/3", "-I", "-S", "-B", "-c", program)
	cmd.Dir = "/"
	cmd.Env = []string{"LC_ALL=C", "TZ=UTC"}
	cmd.ExtraFiles = []*os.File{python}
	cmd.Stdin = bytes.NewReader(suite)
	cmd.WaitDelay = time.Second
	var output limitedReport
	cmd.Stdout = &output
	err = cmd.Run()
	switch {
	case ctx.Err() != nil:
		return nil, ctx.Err()
	case output.limited:
		return nil, app.ErrCoreLimit
	case err != nil:
		var failed *exec.ExitError
		if errors.As(err, &failed) {
			switch failed.ExitCode() {
			case 2:
				return nil, app.ErrCoreInput
			case 3:
				return nil, app.ErrAdminUnavailable
			}
		}
		return nil, err
	default:
		return output.body.Bytes(), nil
	}
}

type limitedReport struct {
	body    bytes.Buffer
	limited bool
}

func (r *limitedReport) Write(body []byte) (int, error) {
	if len(body) > (24<<20)-r.body.Len() {
		r.limited = true
		return 0, app.ErrCoreLimit
	}
	return r.body.Write(body)
}
