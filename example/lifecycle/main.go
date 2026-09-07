// lifecycle is a runnable example of the myaccount/ec2 and myaccount/iam
// clients: list projects, list SSH keys, create a node, poll until it's
// running, log its IP, SSH into it to prove it's reachable, then delete it.
//
// Config is read via viper from a .env file in this directory (gitignored),
// falling back to real process environment variables for anything .env
// doesn't set.
//
// Required:
//
//	E2E_API_KEY       your MyAccount API key (Settings -> API Keys)
//	E2E_AUTH_TOKEN    your MyAccount auth/bearer token (separate from the API key --
//	                  both are required together, see withE2EAuth below)
//	E2E_PROJECT_ID    integer project ID -- NOT the project slug/name. Get it from
//	                  GET /api/v1/iam/multi-crn/'s "last_used_project" field, or the
//	                  "projects" list printed by this script
//	E2E_SSH_KEY_LABEL label of an SSH key already registered with E2E (see README)
//	E2E_SSH_KEY_FILE  path to the matching private key, e.g. ~/.ssh/e2e_network
//
// Optional:
//
//	E2E_LOCATION, E2E_PLAN, E2E_IMAGE   leave all three unset to auto-discover
//	                                    the cheapest available non-GPU Ubuntu
//	                                    plan across every active region (see
//	                                    discoverPlan / ec2.FindAvailablePlans).
//	                                    Set all three to skip discovery and
//	                                    force a specific choice instead.
//	E2E_KEEP=1                          skip the final delete, so you can poke
//	                                    at the node yourself
//
// Run:
//
//	cd example/lifecycle && go run .
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"time"

	ec2 "github.com/go-batteries/e2e-net-sdk/myaccount/ec2"
	iam "github.com/go-batteries/e2e-net-sdk/myaccount/iam"
	"github.com/spf13/viper"
)

const apiBase = "https://api.e2enetworks.com/myaccount"

type config struct {
	apiKey      string
	authToken   string
	projectID   int
	location    string
	plan        string
	image       string
	sshKeyLabel string
	sshKeyFile  string
	keep        bool

	// planPricePerHour is set by discoverPlan; zero if plan/location/image
	// were fixed via E2E_PLAN/E2E_LOCATION/E2E_IMAGE instead of discovered.
	planPricePerHour float32
}

// loadConfig reads config via viper: a .env file in the working directory
// (example/lifecycle/.env, gitignored -- copy your own from the repo root)
// takes precedence, falling back to real process environment variables for
// anything .env doesn't set.
func loadConfig() config {
	v := viper.New()
	v.SetConfigFile(".env")
	v.SetConfigType("env")
	if err := v.ReadInConfig(); err != nil {
		log.Printf("no .env loaded (%v), falling back to process environment", err)
	}
	v.AutomaticEnv()

	return config{
		apiKey:    mustViperString(v, "E2E_API_KEY"),
		authToken: mustViperString(v, "E2E_AUTH_TOKEN"),
		projectID: int(mustViperInt(v, "E2E_PROJECT_ID")),
		// location/plan/image are left blank here on purpose: main() calls
		// ec2.FindAvailablePlans to discover them live rather than trusting
		// a hardcoded value that can go stale (inventory changes; a plan
		// available in one region may be sold out in another). Set
		// E2E_LOCATION/E2E_PLAN/E2E_IMAGE explicitly to skip discovery and
		// force a specific choice instead.
		location:    v.GetString("E2E_LOCATION"),
		plan:        v.GetString("E2E_PLAN"),
		image:       v.GetString("E2E_IMAGE"),
		sshKeyLabel: mustViperString(v, "E2E_SSH_KEY_LABEL"),
		sshKeyFile:  mustViperString(v, "E2E_SSH_KEY_FILE"),
		keep:        v.GetString("E2E_KEEP") == "1",
	}
}

// activeLocations reads the repo root's locations.json -- the canonical,
// hand-maintained list of E2E regions (there is no API to enumerate them;
// see locations.yaml for why). Returns just the "active" ones.
func activeLocations() ([]string, error) {
	raw, err := os.ReadFile("../../locations.json")
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Locations []struct {
			Name   string `json:"name"`
			Status string `json:"status"`
		} `json:"locations"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	var out []string
	for _, loc := range parsed.Locations {
		if loc.Status == "active" {
			out = append(out, loc.Name)
		}
	}
	return out, nil
}

func mustViperString(v *viper.Viper, key string) string {
	val := v.GetString(key)
	if val == "" {
		log.Fatalf("missing required config %s (set it in example/lifecycle/.env or the environment)", key)
	}
	return val
}

func mustViperInt(v *viper.Viper, key string) int64 {
	if !v.IsSet(key) {
		log.Fatalf("missing required config %s (set it in example/lifecycle/.env or the environment)", key)
	}
	n, err := strconv.ParseInt(v.GetString(key), 10, 64)
	if err != nil {
		log.Fatalf("%s must be an integer: %v", key, err)
	}
	return n
}

// withE2EAuth reproduces exactly what E2E's own official e2e-cli sends
// (see e2e_cli/core/apiclient.py in the e2e-cli PyPI package): an
// Authorization: Bearer <auth_token> header, an apikey query parameter,
// and a User-Agent header. E2E's spec only documents the apikey query
// parameter, but every request without ALL THREE together is rejected at
// the API gateway with "API not public" -- before it even reaches E2E's
// own request handlers. This was confirmed by hand against the live API;
// it is not documented anywhere.
func withE2EAuth(cfg config) func(context.Context, *http.Request) error {
	return func(_ context.Context, req *http.Request) error {
		req.Header.Set("Authorization", "Bearer "+cfg.authToken)
		req.Header.Set("User-Agent", "cli-e2e")
		q := req.URL.Query()
		q.Set("apikey", cfg.apiKey)
		req.URL.RawQuery = q.Encode()
		return nil
	}
}

func main() {
	cfg := loadConfig()
	ctx := context.Background()

	iamClient, err := iam.NewClientWithResponses(apiBase)
	if err != nil {
		log.Fatalf("iam client: %v", err)
	}
	ec2Client, err := ec2.NewClientWithResponses(apiBase)
	if err != nil {
		log.Fatalf("ec2 client: %v", err)
	}

	fmt.Println("== projects (CRNs available to this account) ==")
	if err := listProjects(ctx, iamClient, cfg); err != nil {
		fmt.Printf("  (skipped: %v)\n", err)
	}

	if cfg.plan == "" || cfg.location == "" || cfg.image == "" {
		fmt.Println("== available plans (live) ==")
		if err := discoverPlan(ctx, ec2Client, &cfg); err != nil {
			log.Fatalf("discovering an available plan: %v", err)
		}
		fmt.Printf("  chosen: %s / %s in %s (Rs %.2f/hr)\n", cfg.image, cfg.plan, cfg.location, cfg.planPricePerHour)
	}

	fmt.Println("== SSH keys registered on this project ==")
	sshKeyText, err := findSSHKeyText(ctx, ec2Client, cfg)
	if err != nil {
		log.Fatalf("looking up SSH key %q: %v", cfg.sshKeyLabel, err)
	}
	fmt.Printf("  using key %q\n", cfg.sshKeyLabel)

	fmt.Println("== creating node ==")
	nodeID, err := createNode(ctx, ec2Client, cfg, sshKeyText)
	if err != nil {
		log.Fatalf("create node: %v", err)
	}
	fmt.Printf("  node id: %s\n", nodeID)

	if !cfg.keep {
		defer func() {
			fmt.Println("== deleting node ==")
			if err := deleteNode(ctx, ec2Client, cfg, nodeID); err != nil {
				log.Printf("  delete failed: %v (you'll need to remove it from the console)", err)
			} else {
				fmt.Println("  deleted")
			}
		}()
	} else {
		fmt.Println("  E2E_KEEP=1 set, leaving the node running -- delete it yourself when done")
	}

	fmt.Println("== waiting for it to come up ==")
	ip, err := waitForRunning(ctx, ec2Client, cfg, nodeID, 10*time.Minute)
	if err != nil {
		log.Fatalf("wait for running: %v", err)
	}
	fmt.Printf("  running, public ip: %s\n", ip)

	fmt.Println("== logging in over SSH ==")
	if err := sshProbe(cfg, ip); err != nil {
		log.Printf("  ssh probe failed: %v", err)
		fmt.Printf("  try manually: ssh -i %s root@%s (or ubuntu@%s)\n", cfg.sshKeyFile, ip, ip)
	}
}

func listProjects(ctx context.Context, c *iam.ClientWithResponses, cfg config) error {
	editor := withE2EAuth(cfg)
	crnResp, err := c.GetIamMultiCrnWithResponse(ctx, editor)
	if err != nil {
		return err
	}
	if crnResp.JSON200 == nil || crnResp.JSON200.Data == nil || crnResp.JSON200.Data.CrnData == nil {
		return fmt.Errorf("unexpected response (status %d): %s", crnResp.StatusCode(), crnResp.Body)
	}
	for _, crn := range *crnResp.JSON200.Data.CrnData {
		org, email := "", ""
		if crn.OrganisationName != nil {
			org = *crn.OrganisationName
		}
		if crn.Email != nil {
			email = *crn.Email
		}
		crnID := 0
		if crn.Crn != nil {
			crnID = *crn.Crn
		}
		fmt.Printf("  crn=%d org=%q owner=%q\n", crnID, org, email)

		projResp, err := c.GetPbacProjectsHeaderWithResponse(ctx, &iam.GetPbacProjectsHeaderParams{Crn: crnID}, editor)
		if err != nil || projResp.StatusCode() != 200 {
			continue
		}
		fmt.Printf("    projects: %s\n", string(projResp.Body))
	}
	return nil
}

// discoverPlan sweeps every active region (locations.json) with
// ec2.FindAvailablePlans and fills in cfg.location/plan/image with the
// cheapest available non-GPU Ubuntu match. FindAvailablePlans itself makes
// no cheapest/best judgment -- that ranking lives here, in example code,
// not the SDK, since availability and pricing both drift and a hardcoded
// ranking policy has no business being baked into a generated client.
func discoverPlan(ctx context.Context, c *ec2.ClientWithResponses, cfg *config) error {
	locs, err := activeLocations()
	if err != nil {
		return fmt.Errorf("reading locations.json: %w", err)
	}

	plans, err := ec2.FindAvailablePlans(ctx, c, ec2.AvailabilityQuery{
		ProjectID:  cfg.projectID,
		APIKey:     cfg.apiKey,
		Locations:  locs,
		OSName:     "Ubuntu",
		ExcludeGPU: true,
	}, withE2EAuth(*cfg))
	if err != nil {
		return err
	}
	if len(plans) == 0 {
		return fmt.Errorf("no available non-GPU Ubuntu plans found in %v", locs)
	}

	// Plans with 0 bundled disk (E1 family: "...-0DISK-...") need an
	// explicit disk-size parameter this example doesn't send, and fail
	// node creation with "Disk is required for this plan". Skip them
	// rather than model separate disk provisioning here.
	var withDisk []ec2.AvailablePlan
	for _, p := range plans {
		if p.DiskGB > 0 {
			withDisk = append(withDisk, p)
		}
	}
	if len(withDisk) == 0 {
		return fmt.Errorf("no available plans with bundled disk found in %v", locs)
	}

	best := withDisk[0]
	for _, p := range withDisk {
		if p.PricePerHour < best.PricePerHour {
			best = p
		}
	}
	for _, p := range plans {
		fmt.Printf("  %-8s %-60s %2d vCPU %6s GB  Rs %.2f/hr\n", p.Location, p.Plan, p.CPU, p.RAMGB, p.PricePerHour)
	}

	cfg.location = best.Location
	cfg.plan = best.Plan
	cfg.image = best.Image
	cfg.planPricePerHour = best.PricePerHour
	return nil
}

// findSSHKeyText returns the actual public key text (the "ssh-ed25519 AAAA..."
// line), not the key's numeric pk. E2E's create-node API takes literal
// public key strings in `ssh_keys` -- not key IDs looked up from
// GetSshKeys's `pk` field, despite `pk` looking like the natural thing to
// pass. Passing pk (even as a string) is accepted with no error and
// silently installs no key at all: confirmed live on node 344284
// (2026-09-07), which came up Running with no key in authorized_keys and
// had to be deleted.
func findSSHKeyText(ctx context.Context, c *ec2.ClientWithResponses, cfg config) (string, error) {
	resp, err := c.GetSshKeysWithResponse(ctx, &ec2.GetSshKeysParams{
		ProjectId: cfg.projectID,
		Location:  ec2.GetSshKeysParamsLocation(cfg.location),
	}, withE2EAuth(cfg))
	if err != nil {
		return "", err
	}
	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return "", fmt.Errorf("unexpected response (status %d): %s", resp.StatusCode(), resp.Body)
	}
	for _, k := range *resp.JSON200.Data {
		if k.Label != nil && *k.Label == cfg.sshKeyLabel && k.SshKey != nil {
			return *k.SshKey, nil
		}
	}
	return "", fmt.Errorf("no SSH key labelled %q found -- register it first (see repo README)", cfg.sshKeyLabel)
}

func createNode(ctx context.Context, c *ec2.ClientWithResponses, cfg config, sshKeyText string) (string, error) {
	name := fmt.Sprintf("e2e-net-sdk-example-%d", time.Now().Unix())
	body := map[string]any{
		"name":                name,
		"plan":                cfg.plan,
		"image":               cfg.image,
		"region":              cfg.location,
		"default_public_ip":   true,
		"disable_password":    true,
		"number_of_instances": 1,
		// Keys are installed for the OS's default admin user (ubuntu,
		// centos, ...), NOT root -- see sshProbe.
		"ssh_keys": []string{sshKeyText},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	resp, err := c.CreateCommittedWithBodyWithResponse(ctx, &ec2.CreateCommittedParams{
		ProjectId: cfg.projectID,
		Apikey:    cfg.apiKey,
		Location:  cfg.location,
	}, "application/json", bytes.NewReader(raw), withE2EAuth(cfg))
	if err != nil {
		return "", err
	}
	if resp.JSON200 == nil || resp.JSON200.Data == nil || resp.JSON200.Data.NodeCreateResponse == nil {
		return "", fmt.Errorf("unexpected response (status %d): %s", resp.StatusCode(), resp.Body)
	}
	created := *resp.JSON200.Data.NodeCreateResponse
	if len(created) == 0 || created[0].Id == nil {
		return "", fmt.Errorf("create response had no node id: %s", resp.Body)
	}
	return strconv.Itoa(*created[0].Id), nil
}

func waitForRunning(ctx context.Context, c *ec2.ClientWithResponses, cfg config, nodeID string, timeout time.Duration) (string, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		resp, err := c.GetNodeDetailsWithResponse(ctx, nodeID, &ec2.GetNodeDetailsParams{
			ProjectId: cfg.projectID,
			Apikey:    cfg.apiKey,
			Location:  cfg.location,
		}, withE2EAuth(cfg))
		if err != nil {
			return "", err
		}
		if resp.JSON200 != nil && resp.JSON200.Data != nil {
			d := resp.JSON200.Data
			status := ""
			if d.Status != nil {
				status = *d.Status
			}
			fmt.Printf("  status: %s\n", status)
			if status == "Running" && d.PublicIpAddress != nil && *d.PublicIpAddress != "" {
				return *d.PublicIpAddress, nil
			}
		}
		time.Sleep(15 * time.Second)
	}
	return "", fmt.Errorf("timed out waiting for node %s to reach Running", nodeID)
}

// sshProbe tries "root" first, then "ubuntu". E2E's own API docs say keys
// go to "the OS's default admin user (e.g. ubuntu, centos)", not root --
// but confirmed live against a C3/Ubuntu-22.04 node, direct root SSH login
// with the attached key worked fine and "ubuntu" got a permanent
// "Permission denied (publickey)" (not just "not up yet"). Root-first
// matches observed behavior; keep the ubuntu fallback in case a different
// plan/image family behaves as documented.
//
// Separately: the API reporting a node as "Running" doesn't mean sshd is
// accepting connections yet (cloud-init is still finishing); retry with
// backoff rather than failing on the first "Connection refused".
func sshProbe(cfg config, ip string) error {
	users := []string{"root", "ubuntu"}
	var lastErr error
	for attempt := 1; attempt <= 8; attempt++ {
		for _, user := range users {
			cmd := exec.Command("ssh",
				"-i", cfg.sshKeyFile,
				"-o", "StrictHostKeyChecking=accept-new",
				"-o", "ConnectTimeout=8",
				"-o", "BatchMode=yes",
				user+"@"+ip,
				"echo connected to $(hostname) as $(whoami), uptime: $(uptime -p)",
			)
			out, err := cmd.CombinedOutput()
			if err == nil {
				fmt.Printf("  %s\n", string(out))
				return nil
			}
			lastErr = fmt.Errorf("%s@%s: %s: %w", user, ip, string(out), err)
		}
		fmt.Printf("  attempt %d/8: %v\n", attempt, lastErr)
		time.Sleep(15 * time.Second)
	}
	return lastErr
}

func deleteNode(ctx context.Context, c *ec2.ClientWithResponses, cfg config, nodeID string) error {
	resp, err := c.DeleteNodeWithResponse(ctx, nodeID, &ec2.DeleteNodeParams{
		ProjectId: cfg.projectID,
		Apikey:    cfg.apiKey,
		Location:  cfg.location,
	}, withE2EAuth(cfg))
	if err != nil {
		return err
	}
	if resp.StatusCode() >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode(), resp.Body)
	}
	return nil
}
