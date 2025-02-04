package controller

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/types"
)

var (
	successfulStatuses = []int{200, 300, 301, 302, 303}
)

type WebCheckerProbe struct {
	NamespacedName types.NamespacedName
	Host           string
	Path           string
}

func (p *WebCheckerProbe) PerformCheck(ctx context.Context) WebCheckerProbeResult {
	c := http.Client{
		Timeout: 1 * time.Second,
	}

	req, err := http.NewRequest("GET", p.Host+p.Path, nil)
	if err != nil {
		return WebCheckerProbeResult{
			NamespacedName: p.NamespacedName,
			IsSuccessful:   false,
			LastError:      err.Error(),
		}
	}

	resp, err := c.Do(req)
	if err != nil {
		return WebCheckerProbeResult{
			NamespacedName: p.NamespacedName,
			IsSuccessful:   false,
			LastError:      err.Error(),
		}
	}

	defer resp.Body.Close()

	if slices.Contains(successfulStatuses, resp.StatusCode) {
		return WebCheckerProbeResult{
			NamespacedName: p.NamespacedName,
			IsSuccessful:   true,
			LastError:      "",
		}
	}
	return WebCheckerProbeResult{
		NamespacedName: p.NamespacedName,
		IsSuccessful:   false,
		LastError:      fmt.Sprintf("Status code: %d", resp.StatusCode),
	}
}
