package ec2

import (
	"context"
	"fmt"
	"strings"
)

// AvailablePlan is one compute plan confirmed provisionable right now in a
// given location. It's a flattened, hand-picked subset of what OsPlans
// returns per item -- see client.gen.go's OsPlansResponse for the full
// shape if you need more fields.
type AvailablePlan struct {
	Location      string
	Plan          string // pass this as CreateCommittedJSONBody0.Plan
	Image         string // pass this as CreateCommittedJSONBody0.Image
	OSName        string
	OSVersion     string
	Family        string
	CPU           int
	RAMGB         string
	DiskGB        int
	PricePerHour  float32
	PricePerMonth float32
	GPU           bool
}

// AvailabilityQuery scopes a FindAvailablePlans call.
type AvailabilityQuery struct {
	ProjectID int
	APIKey    string

	// Locations to sweep. There is no "list regions" endpoint -- E2E's
	// location set is a closed, hand-maintained list (see locations.json
	// / locations.yaml at the repo root). Pass the "active" ones from
	// there, e.g. []string{"Delhi", "Chennai"}.
	Locations []string

	// OSName filters by OsPlansResponse item .Os.Name, case-insensitive.
	// Empty means no filter (every OS, every plan family: compute, RDS
	// database templates, Kubernetes node templates, everything OsPlans
	// returns for that location).
	OSName string

	// ExcludeGPU drops GPU-backed plans (matched on the image name
	// containing "gpu", or a populated gpu_card_details map -- E2E's
	// plan "family" field is inconsistent about labelling GPU plans as
	// family "GPU"; some GPU plans carry a CPU-sounding family name like
	// "GDC 3rd generation").
	ExcludeGPU bool
}

// FindAvailablePlans sweeps every location in q.Locations and returns every
// plan OsPlans reports as available_inventory_status=true there, after
// applying q.OSName / q.ExcludeGPU. It does not pick a "best" or "cheapest"
// one -- sort or filter the result yourself; availability and pricing both
// change over time and per-account, so baking a ranking into the SDK would
// go stale immediately.
func FindAvailablePlans(ctx context.Context, client *ClientWithResponses, q AvailabilityQuery, editors ...RequestEditorFn) ([]AvailablePlan, error) {
	var out []AvailablePlan

	for _, loc := range q.Locations {
		resp, err := client.OsPlansWithResponse(ctx, &OsPlansParams{
			ProjectId: q.ProjectID,
			Apikey:    q.APIKey,
			Location:  loc,
		}, editors...)
		if err != nil {
			return nil, fmt.Errorf("OsPlans(%s): %w", loc, err)
		}
		if resp.JSON200 == nil || resp.JSON200.Data == nil {
			return nil, fmt.Errorf("OsPlans(%s): unexpected response (status %d): %s", loc, resp.StatusCode(), resp.Body)
		}

		for _, item := range *resp.JSON200.Data {
			if item.AvailableInventoryStatus == nil || !*item.AvailableInventoryStatus {
				continue
			}

			osName, osVersion := "", ""
			if item.Os != nil {
				if item.Os.Name != nil {
					osName = *item.Os.Name
				}
				if item.Os.Version != nil {
					osVersion = *item.Os.Version
				}
			}
			if q.OSName != "" && !strings.EqualFold(osName, q.OSName) {
				continue
			}

			image := ""
			if item.Image != nil {
				image = *item.Image
			}
			isGPU := strings.Contains(strings.ToLower(image), "gpu") ||
				(item.GpuCardDetails != nil && len(*item.GpuCardDetails) > 0)
			if q.ExcludeGPU && isGPU {
				continue
			}

			plan, family := "", ""
			var cpu, disk int
			var ram string
			var priceHr, priceMo float32
			if item.Plan != nil {
				plan = *item.Plan
			}
			if item.Specs != nil {
				if item.Specs.Family != nil {
					family = *item.Specs.Family
				}
				if item.Specs.Cpu != nil {
					cpu = *item.Specs.Cpu
				}
				if item.Specs.DiskSpace != nil {
					disk = *item.Specs.DiskSpace
				}
				if item.Specs.Ram != nil {
					ram = *item.Specs.Ram
				}
				if item.Specs.PricePerHour != nil {
					priceHr = *item.Specs.PricePerHour
				}
				if item.Specs.PricePerMonth != nil {
					priceMo = *item.Specs.PricePerMonth
				}
			}

			out = append(out, AvailablePlan{
				Location:      loc,
				Plan:          plan,
				Image:         image,
				OSName:        osName,
				OSVersion:     osVersion,
				Family:        family,
				CPU:           cpu,
				RAMGB:         ram,
				DiskGB:        disk,
				PricePerHour:  priceHr,
				PricePerMonth: priceMo,
				GPU:           isGPU,
			})
		}
	}

	return out, nil
}
