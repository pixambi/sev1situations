package vault

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Paths and ports inside a Vault node's container.
const (
	binaryPath  = "/usr/local/bin/vault"
	configPath  = "/etc/vault.d/vault.hcl"
	licensePath = "/etc/vault.d/license.hclic"
	dataDir     = "/vault/data"
	logDir      = "/vault/logs"
	// OperationalLog is the file Vault's own log is written to.
	OperationalLog = logDir + "/operational.log"
	// AuditLog is the file the audit device writes to.
	AuditLog = logDir + "/audit.log"

	apiPort     = 8200
	clusterPort = 8201

	// keyShares and keyThreshold are fixed: five shares, any three unseal.
	keyShares    = 5
	keyThreshold = 3
)

// nodeConfig describes one node's configuration file.
type nodeConfig struct {
	// NodeID is the raft node ID, the same as the node's name.
	NodeID string
	// IP is the node's address on the run's network.
	IP string
	// Enterprise adds the license path.
	Enterprise bool
}

// render writes the node's Vault configuration. Clustering is done by the
// deployment with `operator raft join`, not with retry_join, so a node that
// restarts behaves the same as one that was just built.
//
// The mlock system call is disabled because the container does not hold the
// IPC_LOCK capability, which is fine for a training environment (S2).
func (c nodeConfig) render() string {
	var b strings.Builder
	b.WriteString("ui = true\n")
	b.WriteString("disable_mlock = true\n")
	b.WriteString("log_level = \"info\"\n")
	fmt.Fprintf(&b, "api_addr = \"http://%s:%d\"\n", c.IP, apiPort)
	fmt.Fprintf(&b, "cluster_addr = \"http://%s:%d\"\n", c.IP, clusterPort)
	if c.Enterprise {
		fmt.Fprintf(&b, "license_path = %q\n", licensePath)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "storage \"raft\" {\n  path    = %q\n  node_id = %q\n}\n\n", dataDir, c.NodeID)
	fmt.Fprintf(&b, "listener \"tcp\" {\n  address         = \"0.0.0.0:%d\"\n  cluster_address = \"0.0.0.0:%d\"\n  tls_disable     = true\n}\n", apiPort, clusterPort)
	return b.String()
}

// status is the part of `vault status -format=json` we use. The field names are
// what a real Vault 2.2.2 prints: an active node has is_self true, and any
// unsealed node in a cluster names the leader's address.
type status struct {
	Initialized   bool   `json:"initialized"`
	Sealed        bool   `json:"sealed"`
	Version       string `json:"version"`
	HAEnabled     bool   `json:"ha_enabled"`
	IsSelf        bool   `json:"is_self"`
	LeaderAddress string `json:"leader_address"`
	Progress      int    `json:"progress"`
}

// Roles of an unsealed node in a cluster.
const (
	roleActive  = "active"
	roleStandby = "standby"
)

// role says whether an unsealed node is the active node or a standby. A sealed
// node, or one that does not know its leader yet, has no role.
func (s status) role() string {
	switch {
	case s.Sealed || !s.HAEnabled:
		return ""
	case s.IsSelf:
		return roleActive
	case s.LeaderAddress != "":
		return roleStandby
	}
	return ""
}

func parseStatus(out string) (status, error) {
	var s status
	if err := json.Unmarshal([]byte(out), &s); err != nil {
		return s, fmt.Errorf("read the output of vault status: %w", err)
	}
	return s, nil
}

// initResult is the part of `vault operator init -format=json` we use.
type initResult struct {
	UnsealKeys []string `json:"unseal_keys_b64"`
	RootToken  string   `json:"root_token"`
}

func parseInit(out string) (initResult, error) {
	var r initResult
	// The output is not echoed in the error: it holds the keys and the root token.
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		return r, fmt.Errorf("read the output of vault operator init: it is not valid JSON")
	}
	if len(r.UnsealKeys) < keyThreshold || r.RootToken == "" {
		return r, fmt.Errorf("vault operator init returned %d unseal keys and no usable root token", len(r.UnsealKeys))
	}
	return r, nil
}

// peers is the part of `vault operator raft list-peers -format=json` we use.
type peers struct {
	Data struct {
		Config struct {
			Servers []struct {
				NodeID string `json:"node_id"`
				Leader bool   `json:"leader"`
				Voter  bool   `json:"voter"`
			} `json:"servers"`
		} `json:"config"`
	} `json:"data"`
}

func parsePeers(out string) (peers, error) {
	var p peers
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return p, fmt.Errorf("read the output of vault operator raft list-peers: %w", err)
	}
	return p, nil
}

// voters counts the voting members and says which node leads.
func (p peers) voters() (n int, leader string) {
	for _, s := range p.Data.Config.Servers {
		if s.Voter {
			n++
		}
		if s.Leader {
			leader = s.NodeID
		}
	}
	return n, leader
}
