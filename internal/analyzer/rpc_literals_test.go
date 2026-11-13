package analyzer

import (
	"reflect"
	"sort"
	"testing"
)

// A regular expression literal that holds a quote character, such as /\'/g,
// is read as the start of a string, which puts every quote after it on the
// same line out of step. The RPC pass must still find the paths the endpoint
// scanner reports.
const quoteInRegexp = `function esc(a){return String(a).replace(/&/g,"&amp;").replace(/\'/g,"&apos;")};`

func sortedCopy(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	return result
}

func TestRPCPathsAfterAQuoteInARegexpAreMethodsNotEndpoints(t *testing.T) {
	source := quoteInRegexp + `x.get(base+"/acme.v1.Billing/Charge",y);fetch("/rpc/acme.v1.Billing/Refund");` +
		`new Ctor("aB3xYz",Req,Res,[a,!1,"/acme.v1.Shipping/Track"]);new Ctor("Qw3Rt",Req,class extends B{},[a,!1,"/Ledger.Post"]);` +
		`fetch("/Settings/Profile");fetch("/acme.Billing/charge")`
	result := Analyze([]byte(source))
	if want := []string{"Billing.Charge", "Billing.Refund", "Ledger.Post", "Shipping.Track"}; !reflect.DeepEqual(result.RPCMethods, want) {
		t.Fatalf("methods = %#v, want %#v", result.RPCMethods, want)
	}
	if want := map[string]string{"Shipping.Track": "aB3xYz", "Ledger.Post": "Qw3Rt"}; !reflect.DeepEqual(result.RPCWireIDs, want) {
		t.Errorf("wire ids = %#v, want %#v", result.RPCWireIDs, want)
	}
	if got := result.RPCPaths["Billing.Refund"]; got != "/rpc/acme.v1.Billing/Refund" {
		t.Errorf("path = %q", got)
	}
	// The look-alike with a lower-case method stays an endpoint.
	if want := []string{"/acme.Billing/charge", "/Settings/Profile"}; !reflect.DeepEqual(sortedCopy(result.Endpoints), sortedCopy(want)) {
		t.Errorf("endpoints = %#v, want %#v", result.Endpoints, want)
	}
	// The overview reads the methods from the source alone, and finds the same.
	if got := RPCMethods(source); !reflect.DeepEqual(got, result.RPCMethods) {
		t.Errorf("RPCMethods = %#v", got)
	}
}

func TestAnEndpointThatIsAnRPCPathIsAnRPCMethod(t *testing.T) {
	// The endpoint scanner trims a literal; the RPC pass alone would reject
	// " /acme.Billing/Charge " for its spaces. Either way it is one method.
	result := Analyze([]byte(`x(" /acme.Billing/Charge ");y("/acme.Billing/Charge)");z("/Settings/Profile")`))
	if !reflect.DeepEqual(result.RPCMethods, []string{"Billing.Charge"}) || !reflect.DeepEqual(result.Endpoints, []string{"/Settings/Profile"}) {
		t.Fatalf("got methods %#v, endpoints %#v", result.RPCMethods, result.Endpoints)
	}
	if result.RPCPaths["Billing.Charge"] != "/acme.Billing/Charge" {
		t.Errorf("paths = %#v", result.RPCPaths)
	}
	for _, endpoint := range result.Endpoints {
		if _, _, ok := RPCPath(endpoint); ok {
			t.Errorf("endpoint %q is an RPC path", endpoint)
		}
	}
}
