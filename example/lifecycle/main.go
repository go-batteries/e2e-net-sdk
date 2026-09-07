// lifecycle is a runnable example of the myaccount/ec2 and myaccount/iam
// clients: list projects, list SSH keys, create a node, poll until it's
// running, log its IP, SSH into it to prove it's reachable, then delete it.
//
// Required env vars:
//
//	E2E_API_KEY       your MyAccount API key (Settings -> API Keys)
//	E2E_PROJECT_ID    integer project ID (Settings -> IAM, or from ListProjects below)
//	E2E_SSH_KEY_LABEL label of an SSH key already registered with E2E (see README)
//	E2E_SSH_KEY_FILE  path to the matching private key, e.g. ~/.ssh/e2e_network
//
// Optional:
//
//	E2E_LOCATION  default "Delhi"
//	E2E_PLAN      default "C3.4GB" -- check ListPlans in the console for valid values
//	E2E_IMAGE     default "Ubuntu-24.04-Distro"
//	E2E_KEEP=1    skip the final delete, so you can poke at the node yourself
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
)

const apiBase = "https://api.e2enetworks.com/myaccount"

type config struct {
	apiKey      string
	projectID   int
	location    string
	plan        string
	image       string
	sshKeyLabel string
	sshKeyFile  string
	keep        bool
}

func loadConfig() config {
	projectID, err := strconv.Atoi(mustEnv("E2E_PROJECT_ID"))
	if err != nil {
		log.Fatalf("E2E_PROJECT_ID must be an integer: %v", err)
	}
	return config{
		apiKey:      mustEnv("E2E_API_KEY"),
		projectID:   projectID,
		location:    envOr("E2E_LOCATION", "Delhi"),
		plan:        envOr("E2E_PLAN", "C3.4GB"),
		image:       envOr("E2E_IMAGE", "Ubuntu-24.04-Distro"),
		sshKeyLabel: mustEnv("E2E_SSH_KEY_LABEL"),
		sshKeyFile:  mustEnv("E2E_SSH_KEY_FILE"),
		keep:        os.Getenv("E2E_KEEP") == "1",
	}
}

// withAPIKeyQuery appends apikey=<key> to the query string. A few endpoints
// (ssh keys list, CRN list) don't declare an apikey parameter in E2E's spec
// at all -- this papers over that gap rather than silently sending
// unauthenticated requests to them.
func withAPIKeyQuery(apiKey string) func(context.Context, *http.Request) error {
	return func(_ context.Context, req *http.Request) error {
		q := req.URL.Query()
		q.Set("apikey", apiKey)
		req.URL.RawQuery = q.Encode()
		return nil
	}
}

func mustEnv(name string) string {
	v := os.Getenv(name)
	if v == "" {
		log.Fatalf("missing required env var %s", name)
	}
	return v
}

func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return fallback
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

	fmt.Println("== projects (CRNs available to this API key) ==")
	if err := listProjects(ctx, iamClient, cfg.apiKey); err != nil {
		// Not fatal -- this endpoint is undocumented as to its own auth
		// requirements in E2E's spec. The rest of the script uses
		// E2E_PROJECT_ID directly regardless.
		fmt.Printf("  (skipped: %v)\n", err)
	}

	fmt.Println("== SSH keys registered on this project ==")
	sshKeyPk, err := findSSHKeyPk(ctx, ec2Client, cfg)
	if err != nil {
		log.Fatalf("looking up SSH key %q: %v", cfg.sshKeyLabel, err)
	}
	fmt.Printf("  using key %q (pk=%d)\n", cfg.sshKeyLabel, sshKeyPk)

	fmt.Println("== creating node ==")
	nodeID, err := createNode(ctx, ec2Client, cfg, sshKeyPk)
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
		fmt.Printf("  try manually: ssh -i %s root@%s\n", cfg.sshKeyFile, ip)
	}
}

func listProjects(ctx context.Context, c *iam.ClientWithResponses, apiKey string) error {
	editor := withAPIKeyQuery(apiKey)
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

func findSSHKeyPk(ctx context.Context, c *ec2.ClientWithResponses, cfg config) (int, error) {
	resp, err := c.GetSshKeysWithResponse(ctx, &ec2.GetSshKeysParams{
		ProjectId: cfg.projectID,
		Location:  ec2.GetSshKeysParamsLocation(cfg.location),
	}, withAPIKeyQuery(cfg.apiKey))
	if err != nil {
		return 0, err
	}
	if resp.JSON200 == nil || resp.JSON200.Data == nil {
		return 0, fmt.Errorf("unexpected response (status %d): %s", resp.StatusCode(), resp.Body)
	}
	for _, k := range *resp.JSON200.Data {
		if k.Label != nil && *k.Label == cfg.sshKeyLabel && k.Pk != nil {
			return *k.Pk, nil
		}
	}
	return 0, fmt.Errorf("no SSH key labelled %q found -- register it first (see repo README)", cfg.sshKeyLabel)
}

func createNode(ctx context.Context, c *ec2.ClientWithResponses, cfg config, sshKeyPk int) (string, error) {
	name := fmt.Sprintf("e2e-net-sdk-example-%d", time.Now().Unix())
	body := map[string]any{
		"name":                name,
		"plan":                cfg.plan,
		"image":               cfg.image,
		"region":              cfg.location,
		"default_public_ip":   true,
		"disable_password":    true,
		"number_of_instances": 1,
		"ssh_keys":            []string{strconv.Itoa(sshKeyPk)},
	}
	raw, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	resp, err := c.CreateCommittedWithBodyWithResponse(ctx, &ec2.CreateCommittedParams{
		ProjectId: cfg.projectID,
		Apikey:    cfg.apiKey,
		Location:  cfg.location,
	}, "application/json", bytes.NewReader(raw))
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
		})
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

func sshProbe(cfg config, ip string) error {
	cmd := exec.Command("ssh",
		"-i", cfg.sshKeyFile,
		"-o", "StrictHostKeyChecking=accept-new",
		"-o", "ConnectTimeout=10",
		"root@"+ip,
		"echo connected to $(hostname), uptime: $(uptime -p)",
	)
	out, err := cmd.CombinedOutput()
	fmt.Printf("  %s\n", string(out))
	return err
}

func deleteNode(ctx context.Context, c *ec2.ClientWithResponses, cfg config, nodeID string) error {
	resp, err := c.DeleteNodeWithResponse(ctx, nodeID, &ec2.DeleteNodeParams{
		ProjectId: cfg.projectID,
		Apikey:    cfg.apiKey,
		Location:  cfg.location,
	})
	if err != nil {
		return err
	}
	if resp.StatusCode() >= 300 {
		return fmt.Errorf("status %d: %s", resp.StatusCode(), resp.Body)
	}
	return nil
}
