package vault

import (
	"strings"
	"testing"
)

func TestRenderedConfig(t *testing.T) {
	got := nodeConfig{NodeID: "node-2", IP: "10.89.1.3"}.render()

	for _, want := range []string{
		`api_addr = "http://10.89.1.3:8200"`,
		`cluster_addr = "http://10.89.1.3:8201"`,
		`node_id = "node-2"`,
		`path    = "/vault/data"`,
		`address         = "0.0.0.0:8200"`,
		`cluster_address = "0.0.0.0:8201"`,
		`disable_mlock = true`,
		`storage "raft"`,
	} {
		if !strings.Contains(got, want) {
			t.Errorf("config is missing %q:\n%s", want, got)
		}
	}
	if strings.Contains(got, "license_path") {
		t.Error("a community config names a license")
	}
	if strings.Contains(got, "retry_join") {
		t.Error("joining is done by the deployment, so the config must not also retry_join")
	}
}

func TestEnterpriseConfigNamesTheLicenseFile(t *testing.T) {
	got := nodeConfig{NodeID: "node-1", IP: "10.89.1.2", Enterprise: true}.render()
	if !strings.Contains(got, `license_path = "/etc/vault.d/license.hclic"`) {
		t.Errorf("config = %s", got)
	}
	// The license itself is never in the configuration (R-SET-3): only a path to it.
	if strings.Contains(got, "02MV") {
		t.Error("the config holds license text")
	}
}

// The three outputs below were captured from a real Vault 2.2.2 Raft node.
const (
	realUninitialised = `{"type":"shamir","initialized":false,"sealed":true,"version":"2.2.2","storage_type":"raft","removed_from_cluster":false,"ha_enabled":true,"active_time":"0001-01-01T00:00:00Z"}`
	realActive        = `{"type":"shamir","initialized":true,"sealed":false,"version":"2.2.2","cluster_name":"vault-cluster-9476ff9e","storage_type":"raft","ha_enabled":true,"is_self":true,"active_time":"2026-11-26T03:12:29.890524992Z","leader_address":"http://127.0.0.1:8200","leader_cluster_address":"https://127.0.0.1:8201","raft_committed_index":38}`
	realStandby       = `{"type":"shamir","initialized":true,"sealed":false,"version":"2.2.2","storage_type":"raft","ha_enabled":true,"active_time":"0001-01-01T00:00:00Z","leader_address":"http://10.89.1.2:8200"}`
)

func TestParseStatusAndRole(t *testing.T) {
	tests := []struct {
		name, json, role string
		sealed, init     bool
	}{
		{"not initialised", realUninitialised, "", true, false},
		{"the active node", realActive, "active", false, true},
		{"a standby node", realStandby, "standby", false, true},
		{"a sealed node that was initialised", `{"initialized":true,"sealed":true,"ha_enabled":true}`, "", true, true},
		{"an unsealed node that does not know its leader yet", `{"initialized":true,"sealed":false,"ha_enabled":true}`, "", false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s, err := parseStatus(tt.json)
			if err != nil {
				t.Fatal(err)
			}
			if s.role() != tt.role || s.Sealed != tt.sealed || s.Initialized != tt.init {
				t.Errorf("status = %+v role %q, want role %q sealed %v initialised %v", s, s.role(), tt.role, tt.sealed, tt.init)
			}
		})
	}
	if _, err := parseStatus("not json"); err == nil {
		t.Error("garbage was accepted")
	}
}

func TestParseInit(t *testing.T) {
	good := `{"unseal_keys_b64":["a","b","c","d","e"],"root_token":"hvs.root"}`
	r, err := parseInit(good)
	if err != nil || len(r.UnsealKeys) != 5 || r.RootToken != "hvs.root" {
		t.Errorf("init = %+v, %v", r, err)
	}

	for _, bad := range []string{`{"unseal_keys_b64":["a"],"root_token":"hvs.root"}`, `{"unseal_keys_b64":["a","b","c"],"root_token":""}`} {
		if _, err := parseInit(bad); err == nil {
			t.Errorf("parseInit(%s) succeeded", bad)
		}
	}

	// The keys and the token must not appear in the error text.
	_, err = parseInit(`{"unseal_keys_b64":["SECRETKEY1"],"root_token":"hvs.SECRET"` /* truncated */)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Errorf("error = %v, want one that does not echo the output (R-QUAL-3)", err)
	}
}

func TestParsePeers(t *testing.T) {
	p, err := parsePeers(`{"data":{"config":{"servers":[
		{"node_id":"node-1","leader":true,"voter":true},
		{"node_id":"node-2","leader":false,"voter":true},
		{"node_id":"node-3","leader":false,"voter":false}]}}}`)
	if err != nil {
		t.Fatal(err)
	}
	if n, leader := p.voters(); n != 2 || leader != "node-1" {
		t.Errorf("voters = %d leader = %q, want 2 and node-1", n, leader)
	}
}
