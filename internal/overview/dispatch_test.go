package overview

import (
	"bytes"
	"strings"
	"testing"
)

func TestSwitchDispatchNamesItsUnitAndCases(t *testing.T) {
	code := `async function handle(req) {
  switch (req.method) {
    case "hello": return hello();
    case "session.start": return start();
    case "session.stop": return stop();
    default: throw new Error("unknown");
  }
}
function kind(v) { switch (typeof v) { case "string": case "number": case "object": return 1; } }
function glob(p) { switch (p) { case "*": case "**/*": case "*.*": return 1; } }
function hello() {} function start() {} function stop() {}
`
	file := Analyze([]Source{{Name: "rpc.js", Data: []byte(code)}}).Files[0]
	if len(file.Dispatches) != 1 {
		t.Fatalf("dispatches: %#v", file.Dispatches)
	}
	d := file.Dispatches[0]
	if d.Kind != "switch" || d.In != "handle" || d.On != "req.method" || d.Count != 3 || d.Cases[1] != "session.start" || d.Line != 2 {
		t.Fatalf("dispatch: %#v", d)
	}
	var output bytes.Buffer
	if err := WriteText(&output, Result{Sources: 1, Files: []File{file}}, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "- handle() · switch on req.method — 3 cases: hello, session.start, session.stop (line 2)") {
		t.Fatalf("report:\n%s", output.String())
	}
}

func TestHandlerMapAndRouteDispatch(t *testing.T) {
	code := `const handlers = {
  open: (msg) => show(msg),
  close() { hide(); },
  "tab.reload": async function () {},
  ping: pong,
};
function onMessage(event) { handlers[event.data.type]?.(event.data); }
function show() {} function hide() {} function pong() {}
app.get("/users", listUsers);
app.post("/users", createUser);
router.delete("/users/:id", removeUser);
cache.get("/users", 1);
`
	file := Analyze([]Source{{Name: "app.js", Data: []byte(code)}}).Files[0]
	var handler, routes *Dispatch
	for i := range file.Dispatches {
		switch file.Dispatches[i].Kind {
		case "handler map handlers":
			handler = &file.Dispatches[i]
		case "routes":
			routes = &file.Dispatches[i]
		}
	}
	if handler == nil || handler.Count != 4 || handler.On != "event.data.type" || handler.In != "onMessage" || strings.Join(handler.Cases, ",") != "open,close,tab.reload,ping" {
		t.Fatalf("handler map: %#v", file.Dispatches)
	}
	if routes == nil || routes.On != "app" || strings.Join(routes.Cases, ",") != "GET /users,POST /users" {
		t.Fatalf("routes: %#v", file.Dispatches)
	}
}

func TestDuplicateInlinedDispatchIsReportedOnce(t *testing.T) {
	block := `function f%d(k){switch(k){case "frame":case "nth":case "role":return 1}}`
	code := strings.ReplaceAll(block, "%d", "1") + "\n" + strings.ReplaceAll(block, "%d", "2")
	file := Analyze([]Source{{Name: "dup.js", Data: []byte(code)}}).Files[0]
	if len(file.Dispatches) != 1 {
		t.Fatalf("dispatches: %#v", file.Dispatches)
	}
}
