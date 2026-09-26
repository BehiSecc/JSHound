package overview

import (
	"bytes"
	"strings"
	"testing"
)

func TestWebpackFactoryOverviewShowsObservedModuleFlow(t *testing.T) {
	code := `(self.webpackChunkdemo=self.webpackChunkdemo||[]).push([[42],{
101:(module,exports,load)=>{
  const base="/passport/web";
  const routes=[base+"/auth/login/",base+"/account/info/",base+"/email/send_code/",base+"/password/reset/",base+"/auth/verify/"];
  const TTVerifyCenter={render(){},getFp(){}};
  const sdk={login(){},register(){},qrcodeLogin(){},forgetPwd(){}};
  const verify_center_decision_conf=true, withCredentials=true, interceptors=true;
  function show({requestConfig,verifyData}) {
    TTVerifyCenter.render({verifyData,successCb:function(){
      return TTVerifyCenter.getFp().then(fp=>request({...requestConfig,params:{fp,isResend:true}}));
    }});
  }
  load(202);
},
202:function(module,exports,load){const marker="__coverage__";function qaPreReport(){} }
}]);`
	file := Analyze([]Source{{Name: "chunk.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || file.Format != "loader chunk" || len(file.Bundle.Modules) < 2 {
		t.Fatalf("missing factory map: %#v", file)
	}
	module := file.Bundle.Modules[0]
	if module.ID != "101" || module.Role != "account and verification SDK" || len(module.Flow) != 3 || !has(module.Dependencies, "202") {
		t.Fatalf("missing account flow or dependency: %#v", module)
	}
	if !strings.Contains(strings.Join(module.Facts, " "), "/passport/web") || !strings.Contains(file.Diagram, "success callback") {
		t.Fatalf("missing routes or diagram: %#v", file)
	}
	var plain, colored bytes.Buffer
	result := Result{Sources: 1, Files: []File{file}}
	if err := WriteText(&plain, result, TextOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := WriteText(&colored, result, TextOptions{Color: true}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(plain.String(), "\x1b[") || !strings.Contains(colored.String(), "\x1b[1;36m") ||
		!strings.Contains(colored.String(), "```mermaid\nflowchart LR\n") {
		t.Fatalf("terminal color or plain Markdown broken:\n%s", colored.String())
	}
}

func TestWebpackStartupDistinguishesLocalAndExternalModules(t *testing.T) {
	code := `(self.webpackChunkdemo=self.webpackChunkdemo||[]).push([[7],{
101:(m,x,r)=>{window.__assetPrefix__="https://cdn.example/assets"},
202:(m,x,r)=>{(0,React.createRoot)(document.getElementById("root")).render(App())}
},e=>{var run=id=>e(e.s=id);e.O(0,[5,6],()=>(run(101),run(202),run(303)));e.O()}]);`
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || file.Bundle.Startup == nil || len(file.Bundle.Startup.Modules) != 3 ||
		!file.Bundle.Startup.Modules[0].DefinedHere || file.Bundle.Startup.Modules[0].AssetPrefix != "https://cdn.example/assets" ||
		file.Bundle.Startup.Modules[1].MountedAt != "#root" || file.Bundle.Startup.Modules[2].DefinedHere ||
		!strings.Contains(file.Diagram, "startup callback") {
		t.Fatalf("missing startup path: %#v", file)
	}
}

func TestWebpackFunctionStartup(t *testing.T) {
	code := `(self.webpackChunkdemo=self.webpackChunkdemo||[]).push([[8410],{129:function(){(0,r.createRoot)(document.getElementById("root")).render(App())},35225:function(){window.__assetPrefix__="https://cdn.example"}},function(e){var t=function(t){return e(e.s=t)};e.O(0,["1796","2285"],function(){return t(35225),t(129)}),e.O()}]);`
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || file.Bundle.Startup == nil || len(file.Bundle.Startup.Modules) != 2 || file.Bundle.Startup.Modules[1].MountedAt != "#root" {
		t.Fatalf("function callback startup: %#v", file)
	}
}

func TestOrdinaryQueueIsNotWebpackChunk(t *testing.T) {
	code := `window.queue.push([[1],{2:()=>console.log("ready")}]);`
	file := Analyze([]Source{{Name: "queue.js", Data: []byte(code)}}).Files[0]
	if file.Bundle != nil || file.Format == "loader chunk" {
		t.Fatalf("ordinary push mistaken for Webpack: %#v", file)
	}
}

func TestConciseArrowFactoryIsCounted(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{2:(m,e,r)=>r(3),3:(m,e,r)=>{}}]);`
	chunk := findChunk(lex(code))
	if chunk == nil || chunk.modules != 2 {
		t.Fatalf("concise arrow factory was omitted: %#v", chunk)
	}
}

func TestStartupSkipsUncalledNestedRequires(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{2:(m,e,r)=>{}},r=>{function later(){r(2)}}]);`
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || file.Bundle.Startup != nil {
		t.Fatalf("uncalled require became startup: %#v", file.Bundle)
	}
}

func TestStartupSkipsUncalledConciseArrow(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{2:(m,e,r)=>{}},r=>{const later=()=>r(2)}]);`
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || file.Bundle.Startup != nil {
		t.Fatalf("uncalled arrow became startup: %#v", file.Bundle)
	}
}

func TestConciseRuntimeCallbackRequiresEntry(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{2:(m,e,r)=>{}},r=>r(2)]);`
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || file.Bundle.Startup == nil || len(file.Bundle.Startup.Modules) != 1 || file.Bundle.Startup.Modules[0].ID != "2" {
		t.Fatalf("concise runtime callback was missed: %#v", file.Bundle)
	}
}

func TestStartupIgnoresUnrelatedScheduler(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{2:(m,e,r)=>{}},r=>{other.O(0,[5],()=>foo(2))}]);`
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || file.Bundle.Startup != nil {
		t.Fatalf("unrelated callback became startup: %#v", file.Bundle)
	}
}

func TestStartupFactsRequireActualGlobalAndMountChain(t *testing.T) {
	code := `(self.webpackChunkapp=self.webpackChunkapp||[]).push([[1],{2:(m,e,r)=>{
const __assetPrefix__="local"; obj.__assetPrefix__="other";
const node=createRoot(foo); document.getElementById("root"); other.render("x");
}},r=>{r(2)}]);`
	file := Analyze([]Source{{Name: "main.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || file.Bundle.Startup == nil || len(file.Bundle.Startup.Modules) != 1 ||
		file.Bundle.Startup.Modules[0].AssetPrefix != "" || file.Bundle.Startup.Modules[0].MountedAt != "" {
		t.Fatalf("unrelated tokens became startup facts: %#v", file.Bundle)
	}
}

func TestChallengeObjectWithoutCallDoesNotClaimRetry(t *testing.T) {
	code := `(self.webpackChunkdemo=self.webpackChunkdemo||[]).push([[9],{
101:(m,x,r)=>{const routes=["/passport/web/auth/login/","/passport/web/account/info/","/passport/web/email/send_code/","/passport/web/password/reset/","/passport/web/auth/verify/"];
const TTVerifyCenter={getFp(){}};const verify_center_decision_conf=true;
function show({requestConfig,verifyData}){return TTVerifyCenter.render({successCb:function(){TTVerifyCenter.getFp();return {fp:3,isResend:true}}})}}
}]);`
	file := Analyze([]Source{{Name: "chunk.js", Data: []byte(code)}}).Files[0]
	if file.Bundle == nil || len(file.Bundle.Modules) == 0 || len(file.Bundle.Modules[0].Flow) != 0 {
		t.Fatalf("invented retry: %#v", file.Bundle)
	}
}

func TestCompiledOverviewGroupsRPCsAndSkipsAssets(t *testing.T) {
	code := `"use strict";this.default_wallet=this.default_wallet||{};(function(_){
const methods=["/ClaimService.CreateClaim","/ClaimService.GetClaim","/ClaimService.ListClaims",
"/PartnerService.CreatePartner","/PartnerService.GetPartner"];
const paths=["/earnings/overview","/earnings/overview/claims","/earnings/icon.svg"];
}).call(this,this.default_wallet);` + strings.Repeat("var a=1;", 600)
	file := Analyze([]Source{{Name: "wallet.js", Data: []byte(code)}}).Files[0]
	if file.Compiled == nil || len(file.Compiled.Services) != 2 || file.Compiled.Services[0].Name != "ClaimService" || file.Compiled.Services[0].Count != 3 {
		t.Fatalf("RPC grouping: %#v", file)
	}
	if len(file.Compiled.Paths) != 1 || file.Compiled.Paths[0].Prefix != "/earnings/overview" || file.Compiled.Paths[0].Count != 2 {
		t.Fatalf("path grouping should exclude assets: %#v", file.Compiled.Paths)
	}
	if file.Diagram != "" {
		t.Fatalf("containment-only diagram adds no flow: %s", file.Diagram)
	}
}

func TestCompiledOverviewShowsModuleManagerBootAndGenericPathGroup(t *testing.T) {
	code := `(function(_){const paths=["/api/v1/users","/api/v1/orders","/api/v1/orders/42"];
_._ModuleManager_initialize=function(){};_._ModuleManager_initialize("base",[])}).call(this,this.default_app);` + strings.Repeat("var a=1;", 600)
	file := Analyze([]Source{{Name: "app.js", Data: []byte(code)}}).Files[0]
	if file.Compiled == nil || file.Compiled.Boot == "" || len(file.Compiled.Paths) != 1 || file.Compiled.Paths[0].Prefix != "/api/v1" {
		t.Fatalf("compiled boot or path family: %#v", file)
	}
}

func TestCompiledScriptFindsFinalBootstrapIIFE(t *testing.T) {
	code := strings.Repeat("function helper(){return 1;}", 200) +
		`const paths=["/api/v1/foo","/api/v1/bar"];(function(app){ app.start() })(window.app);`
	file := Analyze([]Source{{Name: "app.js", Data: []byte(code)}}).Files[0]
	if file.Compiled == nil || len(file.EntryPoints) != 1 || file.EntryPoints[0].Action != "invokes a final top-level IIFE" {
		t.Fatalf("final bootstrap was missed: %#v", file)
	}
}

func TestNestedFinalIIFEIsNotPageStartup(t *testing.T) {
	code := strings.Repeat("var a=1;", 600) + "function library(){return (function(){return 1})()}"
	file := Analyze([]Source{{Name: "app.js", Data: []byte(code)}}).Files[0]
	if len(file.EntryPoints) != 0 {
		t.Fatalf("nested IIFE became page startup: %#v", file.EntryPoints)
	}
}

func TestUncalledCompiledFunctionIsNotBoot(t *testing.T) {
	code := strings.Repeat("var a=1;", 600) + `function never(){_ModuleManager_initialize("base",[])}`
	file := Analyze([]Source{{Name: "app.js", Data: []byte(code)}}).Files[0]
	if file.Compiled != nil && file.Compiled.Boot != "" {
		t.Fatalf("uncalled module-manager call became boot: %#v", file.Compiled)
	}
}

func TestRouteFamiliesKeepUsefulCommonPrefix(t *testing.T) {
	paths := []string{
		"/tiktok/artist_api/ttfa/account/send_code/",
		"/tiktok/artist_api/ttfa/artist/detail/v1",
		"/tiktok/artist_api/ttfa/feedback/create/v1",
		"/tiktok/artist_api/ttfa/pre_save/campaign",
		"/tiktok/user/artist_zone/data/insight/v1",
	}
	families := routeFamilies(paths)
	if len(families) == 0 || families[0] != "/tiktok/artist_api/ttfa (4)" {
		t.Fatalf("route grouping hid the useful prefix: %#v", families)
	}
}
