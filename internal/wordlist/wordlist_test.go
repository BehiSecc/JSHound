package wordlist

import (
	"slices"
	"testing"
)

func TestBuild(t *testing.T) {
	got := Build([]string{"/api/v2/users/:userId"}, []string{"https://target.test/admin/login?next=x"}, []string{"assets/config.json"}, []string{"next", "userId"}, []string{"InvoiceService.listInvoices"})
	for _, want := range []string{"admin", "api", "config", "login", "next", "userid", "users", "invoiceservice", "listinvoices"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q in %#v", want, got)
		}
	}
	if slices.Contains(got, "v2") == false {
		t.Errorf("expected version segment in %#v", got)
	}
}

func TestBuildReadsRPCMethodsWithoutAServiceSuffix(t *testing.T) {
	got := Build(nil, nil, nil, nil, []string{"Billing.Charge", "JSONRPC.tools/call"})
	for _, want := range []string{"billing", "charge", "jsonrpc", "tools", "call"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q in %#v", want, got)
		}
	}
	if slices.Contains(got, "tools/call") {
		t.Errorf("a JSON-RPC method must be split into words: %#v", got)
	}
}
