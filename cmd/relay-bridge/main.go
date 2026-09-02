// relay-bridge installs approved Relay manifests into an unmodified Hub's
// thirdparty mount. It does not download arbitrary Python or restart services.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/Cylunex/shadow-relay/internal/bookplugin"
	"github.com/Cylunex/shadow-relay/internal/fetch"
	"github.com/Cylunex/shadow-relay/internal/security"
)

func run() error {
	root := flag.String("output", "", "Hub thirdparty plugin mount")
	interval := flag.Duration("interval", 0, "poll interval, zero for one sync; minimum 5m")
	flag.Parse()
	if *root == "" || *interval < 0 || (*interval > 0 && *interval < 5*time.Minute) {
		return errors.New("provide --output and use --interval 0 or at least 5m")
	}
	subscription := os.Getenv("RELAY_BRIDGE_SUBSCRIPTION_URL")
	if e := security.SafeURL(subscription); e != nil {
		return errors.New("set RELAY_BRIDGE_SUBSCRIPTION_URL to a scoped hub/plugins.json binding URL")
	}
	f, e := fetch.New(os.Getenv("RELAY_BRIDGE_TRUSTED_CIDRS"))
	if e != nil {
		return e
	}
	policy := fetch.Policy{Network: "internet", Trust: "reviewed"}
	if network := os.Getenv("RELAY_BRIDGE_NETWORK"); network != "" && network != "internet" && network != "trusted-lan" {
		return errors.New("invalid subscription network")
	}
	if os.Getenv("RELAY_BRIDGE_NETWORK") == "trusted-lan" {
		policy = fetch.Policy{Network: "trusted-lan", Trust: "trusted"}
	}
	hubPolicy := policy
	if network := os.Getenv("RELAY_BRIDGE_HUB_NETWORK"); network != "" {
		if network != "internet" && network != "trusted-lan" {
			return errors.New("invalid Hub network")
		}
		hubPolicy = fetch.Policy{Network: network, Trust: "trusted"}
	}
	hubURL := os.Getenv("RELAY_BRIDGE_HUB_URL")
	var headers map[string]string
	if raw := os.Getenv("RELAY_BRIDGE_HUB_HEADERS"); raw != "" {
		if json.Unmarshal([]byte(raw), &headers) != nil {
			return errors.New("invalid Hub headers JSON")
		}
		if e := security.ValidateHeaders(headers); e != nil {
			return e
		}
	}
	if hubURL != "" {
		if e := security.SafeURL(hubURL); e != nil {
			return e
		}
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	etag := ""
	// Retry reload on startup even when a previous process installed the files
	// successfully but exited before Hub acknowledged the reload.
	reloadPending := hubURL != ""
	for {
		err := func() error {
			request, cancel := context.WithTimeout(ctx, 90*time.Second)
			defer cancel()
			h := map[string]string{}
			if etag != "" {
				h["If-None-Match"] = etag
			}
			res, e := f.Get(request, subscription, policy, h, 8<<20, false)
			if e != nil {
				return e
			}
			if res.Status != 304 {
				var report bookplugin.Report
				d := json.NewDecoder(strings.NewReader(string(res.Body)))
				d.DisallowUnknownFields()
				if d.Decode(&report) != nil || d.Decode(new(any)) != io.EOF {
					return errors.New("invalid plugin manifest")
				}
				result, e := bookplugin.Install(*root, report)
				if e != nil {
					return e
				}
				reloadPending = reloadPending || result.Installed > 0 || result.Removed > 0
				fmt.Printf("plugins installed=%d removed=%d unchanged=%d unsupported=%d\n", result.Installed, result.Removed, result.Unchanged, report.Unsupported)
				etag = res.ETag
			}
			if reloadPending && hubURL != "" {
				res, e := f.PostJSON(request, strings.TrimRight(hubURL, "/")+"/api/console/plugins/reload", hubPolicy, headers, []byte("{}"), 1<<20)
				if e != nil {
					return e
				}
				var v struct {
					Reloaded bool `json:"reloaded"`
				}
				if json.Unmarshal(res.Body, &v) != nil || !v.Reloaded {
					return errors.New("Hub did not confirm reload")
				}
				reloadPending = false
			} else if reloadPending {
				fmt.Println("plugin files ready; reload plugins in the Hub console")
				reloadPending = false
			}
			return nil
		}()
		if *interval == 0 {
			return err
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "bridge sync failed; check credentials, network policy and ownership (URLs omitted)")
		}
		timer := time.NewTimer(*interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
func main() {
	if e := run(); e != nil {
		fmt.Fprintln(os.Stderr, e)
		os.Exit(1)
	}
}
