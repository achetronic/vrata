// Package e2e runs end-to-end tests for the controller against a live
// kind cluster with Gateway API CRDs and a running Vrata control plane.
//
// Requirements:
//   - kubectl pointing to a kind cluster with Gateway API CRDs installed
//   - Vrata control plane on localhost:8080
package e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/achetronic/vrata/clients/controller/internal/batcher"
	"github.com/achetronic/vrata/clients/controller/internal/mapper"
	"github.com/achetronic/vrata/clients/controller/internal/reconciler"
	"github.com/achetronic/vrata/clients/controller/internal/vrata"

	gwapiv1 "sigs.k8s.io/gateway-api/apis/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/util/homedir"
	"path/filepath"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

const vrataAPI = "http://localhost:8080/api/v1"

func vrataGet(t *testing.T, path string) []byte {
	t.Helper()
	resp, err := http.Get(vrataAPI + path)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data
}

func vrataPost(t *testing.T, path string, body any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	resp, err := http.Post(vrataAPI+path, "application/json", bytes.NewReader(b))
	if err != nil {
		t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	var result map[string]any
	json.Unmarshal(data, &result)
	return result
}

func vrataDelete(t *testing.T, path string) {
	t.Helper()
	req, _ := http.NewRequest("DELETE", vrataAPI+path, nil)
	http.DefaultClient.Do(req)
}

func vrataCleanOwned(t *testing.T) {
	t.Helper()
	client := vrata.NewClient("http://localhost:8080")
	ctx := context.Background()

	groups, _ := client.ListGroups(ctx)
	for _, g := range groups {
		if mapper.IsOwned(g.Name) {
			client.DeleteGroup(ctx, g.ID)
		}
	}
	routes, _ := client.ListRoutes(ctx)
	for _, r := range routes {
		if mapper.IsOwned(r.Name) {
			client.DeleteRoute(ctx, r.ID)
		}
	}
	mws, _ := client.ListMiddlewares(ctx)
	for _, m := range mws {
		if mapper.IsOwned(m.Name) {
			client.DeleteMiddleware(ctx, m.ID)
		}
	}
	dests, _ := client.ListDestinations(ctx)
	for _, d := range dests {
		if mapper.IsOwned(d.Name) {
			client.DeleteDestination(ctx, d.ID)
		}
	}
	listeners, _ := client.ListListeners(ctx)
	for _, l := range listeners {
		if mapper.IsOwned(l.Name) {
			client.DeleteListener(ctx, l.ID)
		}
	}
}

func k8sClient(t *testing.T) runtimeclient.Client {
	t.Helper()
	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
	cfg, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		t.Fatalf("building k8s config: %v", err)
	}
	scheme := runtime.NewScheme()
	clientgoscheme.AddToScheme(scheme)
	gwapiv1.Install(scheme)
	c, err := runtimeclient.New(cfg, runtimeclient.Options{Scheme: scheme})
	if err != nil {
		t.Fatalf("creating k8s client: %v", err)
	}
	return c
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestE2E_Controller_SyncHTTPRoute(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	// Create an HTTPRoute in k8s.
	pathPrefix := gwapiv1.PathMatchPathPrefix
	port := gwapiv1.PortNumber(80)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "e2e-kc-test",
			Namespace: "default",
		},
		Spec: gwapiv1.HTTPRouteSpec{
			Hostnames: []gwapiv1.Hostname{"test.example.com"},
			Rules: []gwapiv1.HTTPRouteRule{
				{
					Matches: []gwapiv1.HTTPRouteMatch{
						{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/api")}},
					},
					BackendRefs: []gwapiv1.HTTPBackendRef{
						{BackendRef: gwapiv1.BackendRef{
							BackendObjectReference: gwapiv1.BackendObjectReference{
								Name: "test-svc",
								Port: &port,
							},
						}},
					},
				},
			},
		},
	}

	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("creating HTTPRoute: %v", err)
	}
	defer kc.Delete(ctx, hr)

	// Run the mapper + reconciler.
	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)

	input := mapper.HTTPRouteInput{
		Name:      hr.Name,
		Namespace: hr.Namespace,
		Hostnames: []string{"test.example.com"},
		Rules: []mapper.RuleInput{
			{
				Matches: []mapper.MatchInput{
					{PathType: "PathPrefix", PathValue: "/api"},
				},
				BackendRefs: []mapper.BackendRefInput{
					{ServiceName: "test-svc", ServiceNamespace: "default", Port: 80, Weight: 1},
				},
			},
		},
	}
	mapped := mapper.MapHTTPRoute(input)
	changes, err := rec.ApplyHTTPRoute(ctx, mapped)
	if err != nil {
		t.Fatalf("apply HTTPRoute: %v", err)
	}
	if changes == 0 {
		t.Error("expected changes from apply")
	}

	// Verify in Vrata.
	routes, err := vrataClient.ListRoutes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range routes {
		if r.Name == "k8s:default/e2e-kc-test/rule-0/match-0" {
			found = true
		}
	}
	if !found {
		t.Error("route not found in Vrata after sync")
	}

	groups, err := vrataClient.ListGroups(ctx)
	if err != nil {
		t.Fatal(err)
	}
	groupFound := false
	for _, g := range groups {
		if g.Name == "k8s:default/e2e-kc-test" {
			groupFound = true
			if len(g.Hostnames) != 1 || g.Hostnames[0] != "test.example.com" {
				t.Errorf("group hostnames: %v", g.Hostnames)
			}
		}
	}
	if !groupFound {
		t.Error("group not found in Vrata after sync")
	}

	dests, err := vrataClient.ListDestinations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	destFound := false
	for _, d := range dests {
		if d.Name == "k8s:default/test-svc:80" {
			destFound = true
			if d.Host != "test-svc.default.svc.cluster.local" {
				t.Errorf("destination host: %q", d.Host)
			}
		}
	}
	if !destFound {
		t.Error("destination not found in Vrata after sync")
	}
}

func TestE2E_Controller_DeleteHTTPRoute(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)

	// Create via reconciler.
	input := mapper.HTTPRouteInput{
		Name: "e2e-kc-delete", Namespace: "default",
		Hostnames: []string{"delete.example.com"},
		Rules: []mapper.RuleInput{
			{
				Matches:     []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/del"}},
				BackendRefs: []mapper.BackendRefInput{{ServiceName: "del-svc", ServiceNamespace: "default", Port: 80, Weight: 1}},
			},
		},
	}
	mapped := mapper.MapHTTPRoute(input)
	if _, err := rec.ApplyHTTPRoute(ctx, mapped); err != nil {
		t.Fatal(err)
	}

	// Delete.
	changes, err := rec.DeleteHTTPRoute(ctx, "default", "e2e-kc-delete")
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if changes == 0 {
		t.Error("expected changes from delete")
	}

	// Verify gone.
	routes, _ := vrataClient.ListRoutes(ctx)
	for _, r := range routes {
		if mapper.IsOwned(r.Name) && r.Name == "k8s:default/e2e-kc-delete/rule-0/match-0" {
			t.Error("route should be deleted")
		}
	}
	dests, _ := vrataClient.ListDestinations(ctx)
	for _, d := range dests {
		if d.Name == "k8s:default/del-svc:80" {
			t.Error("destination should be deleted (refcount 0)")
		}
	}
}

func TestE2E_Controller_SharedDestination(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)

	// Two routes sharing the same destination.
	input1 := mapper.HTTPRouteInput{
		Name: "e2e-shared-a", Namespace: "default",
		Rules: []mapper.RuleInput{
			{
				Matches:     []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/a"}},
				BackendRefs: []mapper.BackendRefInput{{ServiceName: "shared-svc", ServiceNamespace: "default", Port: 80, Weight: 1}},
			},
		},
	}
	input2 := mapper.HTTPRouteInput{
		Name: "e2e-shared-b", Namespace: "default",
		Rules: []mapper.RuleInput{
			{
				Matches:     []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/b"}},
				BackendRefs: []mapper.BackendRefInput{{ServiceName: "shared-svc", ServiceNamespace: "default", Port: 80, Weight: 1}},
			},
		},
	}

	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input1)); err != nil {
		t.Fatal(err)
	}
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input2)); err != nil {
		t.Fatal(err)
	}

	// Delete route A — destination should survive (still used by B).
	if _, err := rec.DeleteHTTPRoute(ctx, "default", "e2e-shared-a"); err != nil {
		t.Fatal(err)
	}

	dests, _ := vrataClient.ListDestinations(ctx)
	found := false
	for _, d := range dests {
		if d.Name == "k8s:default/shared-svc:80" {
			found = true
		}
	}
	if !found {
		t.Error("shared destination should survive after deleting one route")
	}

	// Delete route B — now destination should be gone.
	if _, err := rec.DeleteHTTPRoute(ctx, "default", "e2e-shared-b"); err != nil {
		t.Fatal(err)
	}

	dests, _ = vrataClient.ListDestinations(ctx)
	for _, d := range dests {
		if d.Name == "k8s:default/shared-svc:80" {
			t.Error("shared destination should be deleted after all routes removed")
		}
	}
}

func TestE2E_Controller_BatchSnapshot(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	bat := batcher.New(vrataClient, 500*time.Millisecond, 1000, testLogger())

	// Apply 3 routes, signal batcher for each.
	for i := 0; i < 3; i++ {
		input := mapper.HTTPRouteInput{
			Name: fmt.Sprintf("e2e-batch-%d", i), Namespace: "default",
			Rules: []mapper.RuleInput{
				{
					Matches:     []mapper.MatchInput{{PathType: "PathPrefix", PathValue: fmt.Sprintf("/batch-%d", i)}},
					BackendRefs: []mapper.BackendRefInput{{ServiceName: "batch-svc", ServiceNamespace: "default", Port: 80, Weight: 1}},
				},
			},
		}
		changes, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input))
		if err != nil {
			t.Fatal(err)
		}
		for j := 0; j < changes; j++ {
			bat.Signal(ctx)
		}
	}

	// Wait for debounce to flush.
	time.Sleep(1 * time.Second)

	if bat.Pending() != 0 {
		t.Errorf("expected 0 pending after flush, got %d", bat.Pending())
	}

	// Verify snapshot was created.
	data := vrataGet(t, "/snapshots")
	var snapshots []map[string]any
	json.Unmarshal(data, &snapshots)
	found := false
	for _, s := range snapshots {
		name, _ := s["name"].(string)
		if strings.HasPrefix(name, "vrata-controller-") {
			found = true
			// Clean up.
			if id, ok := s["id"].(string); ok {
				vrataDelete(t, "/snapshots/"+id)
			}
		}
	}
	if !found {
		t.Error("no controller snapshot found after batch flush")
	}
}

func TestE2E_Controller_RedirectFilter(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)

	input := mapper.HTTPRouteInput{
		Name: "e2e-redirect", Namespace: "default",
		Rules: []mapper.RuleInput{
			{
				Matches: []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/old"}},
				Filters: []mapper.FilterInput{
					{Type: "RequestRedirect", RedirectScheme: "https", RedirectCode: 301},
				},
			},
		},
	}

	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	for _, r := range routes {
		if r.Name == "k8s:default/e2e-redirect/rule-0/match-0" {
			if r.Redirect == nil {
				t.Error("expected redirect action")
			}
			if r.Forward != nil {
				t.Error("redirect route should not have forward")
			}
			return
		}
	}
	t.Error("redirect route not found")
}

func strPtr(s string) *string { return &s }

// gatewayHTTPRouteToInput converts a k8s HTTPRoute to the mapper's input type.
func gatewayHTTPRouteToInput(hr *gwapiv1.HTTPRoute) mapper.HTTPRouteInput {
	input := mapper.HTTPRouteInput{Name: hr.Name, Namespace: hr.Namespace}
	for _, h := range hr.Spec.Hostnames {
		input.Hostnames = append(input.Hostnames, string(h))
	}
	for _, rule := range hr.Spec.Rules {
		ri := mapper.RuleInput{}
		for _, match := range rule.Matches {
			mi := mapper.MatchInput{}
			if match.Path != nil {
				if match.Path.Type != nil {
					mi.PathType = string(*match.Path.Type)
				}
				if match.Path.Value != nil {
					mi.PathValue = *match.Path.Value
				}
			}
			if match.Method != nil {
				mi.Method = string(*match.Method)
			}
			for _, hm := range match.Headers {
				hi := mapper.HeaderMatchInput{Name: string(hm.Name), Value: hm.Value}
				if hm.Type != nil {
					hi.Type = string(*hm.Type)
				}
				mi.Headers = append(mi.Headers, hi)
			}
			ri.Matches = append(ri.Matches, mi)
		}
		for _, br := range rule.BackendRefs {
			ns := hr.Namespace
			if br.Namespace != nil {
				ns = string(*br.Namespace)
			}
			port := uint32(0)
			if br.Port != nil {
				port = uint32(*br.Port)
			}
			weight := uint32(1)
			if br.Weight != nil {
				weight = uint32(*br.Weight)
			}
			ri.BackendRefs = append(ri.BackendRefs, mapper.BackendRefInput{
				ServiceName: string(br.Name), ServiceNamespace: ns, Port: port, Weight: weight,
			})
		}
		for _, f := range rule.Filters {
			fi := mapper.FilterInput{Type: string(f.Type)}
			switch f.Type {
			case gwapiv1.HTTPRouteFilterRequestRedirect:
				if f.RequestRedirect != nil {
					if f.RequestRedirect.Scheme != nil {
						fi.RedirectScheme = *f.RequestRedirect.Scheme
					}
					if f.RequestRedirect.Hostname != nil {
						fi.RedirectHost = string(*f.RequestRedirect.Hostname)
					}
					if f.RequestRedirect.StatusCode != nil {
						fi.RedirectCode = uint32(*f.RequestRedirect.StatusCode)
					}
				}
			case gwapiv1.HTTPRouteFilterURLRewrite:
				if f.URLRewrite != nil {
					if f.URLRewrite.Path != nil && f.URLRewrite.Path.ReplacePrefixMatch != nil {
						fi.RewritePathPrefix = *f.URLRewrite.Path.ReplacePrefixMatch
					}
					if f.URLRewrite.Hostname != nil {
						fi.RewriteHostname = string(*f.URLRewrite.Hostname)
					}
				}
			case gwapiv1.HTTPRouteFilterRequestHeaderModifier:
				if f.RequestHeaderModifier != nil {
					for _, h := range f.RequestHeaderModifier.Add {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{Name: string(h.Name), Value: h.Value})
					}
					for _, h := range f.RequestHeaderModifier.Set {
						fi.HeadersToAdd = append(fi.HeadersToAdd, mapper.HeaderValue{Name: string(h.Name), Value: h.Value})
					}
					for _, name := range f.RequestHeaderModifier.Remove {
						fi.HeadersToRemove = append(fi.HeadersToRemove, name)
					}
				}
			}
			ri.Filters = append(ri.Filters, fi)
		}
		input.Rules = append(input.Rules, ri)
	}
	return input
}

// ─── Mapping verification e2e tests ─────────────────────────────────────────

func TestE2E_Controller_MultipleHostnames(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathPrefix := gwapiv1.PathMatchPathPrefix
	port := gwapiv1.PortNumber(80)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-multi-host", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Hostnames: []gwapiv1.Hostname{"fr.example.com", "de.example.com", "es.example.com"},
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches:     []gwapiv1.HTTPRouteMatch{{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/")}}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "web", Port: &port}}}},
			}},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	input := gatewayHTTPRouteToInput(hr)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	groups, _ := vrataClient.ListGroups(ctx)
	for _, g := range groups {
		if g.Name == "k8s:default/e2e-multi-host" {
			if len(g.Hostnames) != 3 {
				t.Errorf("expected 3 hostnames, got %d: %v", len(g.Hostnames), g.Hostnames)
			}
			expected := map[string]bool{"fr.example.com": true, "de.example.com": true, "es.example.com": true}
			for _, h := range g.Hostnames {
				if !expected[h] {
					t.Errorf("unexpected hostname %q", h)
				}
			}
			return
		}
	}
	t.Error("group not found")
}

func TestE2E_Controller_ExactPathMatch(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathExact := gwapiv1.PathMatchExact
	port := gwapiv1.PortNumber(80)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-exact", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches:     []gwapiv1.HTTPRouteMatch{{Path: &gwapiv1.HTTPPathMatch{Type: &pathExact, Value: strPtr("/health")}}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "health-svc", Port: &port}}}},
			}},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(gatewayHTTPRouteToInput(hr))); err != nil {
		t.Fatal(err)
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	for _, r := range routes {
		if r.Name == "k8s:default/e2e-exact/rule-0/match-0" {
			if r.Match["path"] != "/health" {
				t.Errorf("expected exact path /health, got %v", r.Match)
			}
			if _, hasPrefix := r.Match["pathPrefix"]; hasPrefix {
				t.Error("exact path should not have pathPrefix")
			}
			return
		}
	}
	t.Error("route not found")
}

func TestE2E_Controller_MultipleMatchesPerRule(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathPrefix := gwapiv1.PathMatchPathPrefix
	pathExact := gwapiv1.PathMatchExact
	port := gwapiv1.PortNumber(80)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-multi-match", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{
					{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/api")}},
					{Path: &gwapiv1.HTTPPathMatch{Type: &pathExact, Value: strPtr("/health")}},
					{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/admin")}},
				},
				BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "svc", Port: &port}}}},
			}},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(gatewayHTTPRouteToInput(hr))); err != nil {
		t.Fatal(err)
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	found := 0
	for _, r := range routes {
		switch r.Name {
		case "k8s:default/e2e-multi-match/rule-0/match-0":
			if r.Match["pathPrefix"] != "/api" {
				t.Errorf("match-0: expected pathPrefix /api, got %v", r.Match)
			}
			found++
		case "k8s:default/e2e-multi-match/rule-0/match-1":
			if r.Match["path"] != "/health" {
				t.Errorf("match-1: expected path /health, got %v", r.Match)
			}
			found++
		case "k8s:default/e2e-multi-match/rule-0/match-2":
			if r.Match["pathPrefix"] != "/admin" {
				t.Errorf("match-2: expected pathPrefix /admin, got %v", r.Match)
			}
			found++
		}
	}
	if found != 3 {
		t.Errorf("expected 3 routes (one per match), found %d", found)
	}
}

func TestE2E_Controller_MethodMatch(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathPrefix := gwapiv1.PathMatchPathPrefix
	method := gwapiv1.HTTPMethodPost
	port := gwapiv1.PortNumber(80)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-method", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{
					Path:   &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/submit")},
					Method: &method,
				}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "submit-svc", Port: &port}}}},
			}},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(gatewayHTTPRouteToInput(hr))); err != nil {
		t.Fatal(err)
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	for _, r := range routes {
		if r.Name == "k8s:default/e2e-method/rule-0/match-0" {
			methods, ok := r.Match["methods"].([]any)
			if !ok || len(methods) != 1 {
				t.Fatalf("expected methods [POST], got %v", r.Match["methods"])
			}
			if methods[0] != "POST" {
				t.Errorf("expected POST, got %v", methods[0])
			}
			return
		}
	}
	t.Error("route not found")
}

func TestE2E_Controller_HeaderMatch(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathPrefix := gwapiv1.PathMatchPathPrefix
	headerExact := gwapiv1.HeaderMatchExact
	port := gwapiv1.PortNumber(80)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-header", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{
					Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/")},
					Headers: []gwapiv1.HTTPHeaderMatch{{
						Type:  &headerExact,
						Name:  "X-Tenant",
						Value: "acme",
					}},
				}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "tenant-svc", Port: &port}}}},
			}},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(gatewayHTTPRouteToInput(hr))); err != nil {
		t.Fatal(err)
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	for _, r := range routes {
		if r.Name == "k8s:default/e2e-header/rule-0/match-0" {
			headers, ok := r.Match["headers"].([]any)
			if !ok || len(headers) != 1 {
				t.Fatalf("expected 1 header matcher, got %v", r.Match["headers"])
			}
			hm, ok := headers[0].(map[string]any)
			if !ok {
				t.Fatalf("expected header map, got %T", headers[0])
			}
			if hm["name"] != "X-Tenant" || hm["value"] != "acme" {
				t.Errorf("expected X-Tenant=acme, got %v", hm)
			}
			return
		}
	}
	t.Error("route not found")
}

func TestE2E_Controller_WeightedBackends(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathPrefix := gwapiv1.PathMatchPathPrefix
	port := gwapiv1.PortNumber(80)
	w80 := int32(80)
	w20 := int32(20)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-weighted", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/")}}},
				BackendRefs: []gwapiv1.HTTPBackendRef{
					{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "stable", Port: &port}, Weight: &w80}},
					{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "canary", Port: &port}, Weight: &w20}},
				},
			}},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(gatewayHTTPRouteToInput(hr))); err != nil {
		t.Fatal(err)
	}

	dests, _ := vrataClient.ListDestinations(ctx)
	stableFound, canaryFound := false, false
	for _, d := range dests {
		if d.Name == "k8s:default/stable:80" {
			stableFound = true
		}
		if d.Name == "k8s:default/canary:80" {
			canaryFound = true
		}
	}
	if !stableFound || !canaryFound {
		t.Error("expected both stable and canary destinations")
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	for _, r := range routes {
		if r.Name == "k8s:default/e2e-weighted/rule-0/match-0" {
			fwdDests, ok := r.Forward["destinations"].([]any)
			if !ok || len(fwdDests) != 2 {
				t.Fatalf("expected 2 destination refs, got %v", r.Forward["destinations"])
			}
			return
		}
	}
	t.Error("weighted route not found")
}

func TestE2E_Controller_URLRewriteFilter(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathPrefix := gwapiv1.PathMatchPathPrefix
	port := gwapiv1.PortNumber(80)
	rewritePath := "/new"
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-rewrite", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/old")}}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "svc", Port: &port}}}},
				Filters: []gwapiv1.HTTPRouteFilter{{
					Type: gwapiv1.HTTPRouteFilterURLRewrite,
					URLRewrite: &gwapiv1.HTTPURLRewriteFilter{
						Path: &gwapiv1.HTTPPathModifier{
							Type:               gwapiv1.PrefixMatchHTTPPathModifier,
							ReplacePrefixMatch: &rewritePath,
						},
					},
				}},
			}},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(gatewayHTTPRouteToInput(hr))); err != nil {
		t.Fatal(err)
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	for _, r := range routes {
		if r.Name == "k8s:default/e2e-rewrite/rule-0/match-0" {
			rw, ok := r.Forward["rewrite"].(map[string]any)
			if !ok {
				t.Fatal("expected rewrite in forward")
			}
			if rw["path"] != "/new" {
				t.Errorf("expected rewrite path /new, got %v", rw["path"])
			}
			return
		}
	}
	t.Error("rewrite route not found")
}

func TestE2E_Controller_HeaderModifierFilter(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathPrefix := gwapiv1.PathMatchPathPrefix
	port := gwapiv1.PortNumber(80)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-hdr-mod", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Rules: []gwapiv1.HTTPRouteRule{{
				Matches: []gwapiv1.HTTPRouteMatch{{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/")}}},
				BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "svc", Port: &port}}}},
				Filters: []gwapiv1.HTTPRouteFilter{{
					Type: gwapiv1.HTTPRouteFilterRequestHeaderModifier,
					RequestHeaderModifier: &gwapiv1.HTTPHeaderFilter{
						Add: []gwapiv1.HTTPHeader{{Name: "X-Source", Value: "controller"}},
						Remove: []string{"X-Internal"},
					},
				}},
			}},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(gatewayHTTPRouteToInput(hr))); err != nil {
		t.Fatal(err)
	}

	mws, _ := vrataClient.ListMiddlewares(ctx)
	for _, mw := range mws {
		if mw.Name == "k8s:default/e2e-hdr-mod/rule-0/headers" {
			if mw.Type != "headers" {
				t.Errorf("expected type headers, got %q", mw.Type)
			}
			return
		}
	}
	t.Error("header modifier middleware not found")
}

func TestE2E_Controller_DestinationFQDN(t *testing.T) {
	ctx := context.Background()
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)

	input := mapper.HTTPRouteInput{
		Name: "e2e-fqdn", Namespace: "my-namespace",
		Rules: []mapper.RuleInput{{
			Matches:     []mapper.MatchInput{{PathType: "PathPrefix", PathValue: "/"}},
			BackendRefs: []mapper.BackendRefInput{{ServiceName: "my-service", ServiceNamespace: "my-namespace", Port: 8080, Weight: 1}},
		}},
	}
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(input)); err != nil {
		t.Fatal(err)
	}

	dests, _ := vrataClient.ListDestinations(ctx)
	for _, d := range dests {
		if d.Name == "k8s:my-namespace/my-service:8080" {
			if d.Host != "my-service.my-namespace.svc.cluster.local" {
				t.Errorf("expected FQDN, got %q", d.Host)
			}
			if d.Port != 8080 {
				t.Errorf("expected port 8080, got %d", d.Port)
			}
			return
		}
	}
	t.Error("destination with FQDN not found")
}

func TestE2E_Controller_MultipleRules(t *testing.T) {
	ctx := context.Background()
	kc := k8sClient(t)
	vrataCleanOwned(t)
	defer vrataCleanOwned(t)

	pathPrefix := gwapiv1.PathMatchPathPrefix
	port80 := gwapiv1.PortNumber(80)
	port8080 := gwapiv1.PortNumber(8080)
	hr := &gwapiv1.HTTPRoute{
		ObjectMeta: metav1.ObjectMeta{Name: "e2e-multi-rule", Namespace: "default"},
		Spec: gwapiv1.HTTPRouteSpec{
			Hostnames: []gwapiv1.Hostname{"app.example.com"},
			Rules: []gwapiv1.HTTPRouteRule{
				{
					Matches:     []gwapiv1.HTTPRouteMatch{{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/api")}}},
					BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "api-svc", Port: &port80}}}},
				},
				{
					Matches:     []gwapiv1.HTTPRouteMatch{{Path: &gwapiv1.HTTPPathMatch{Type: &pathPrefix, Value: strPtr("/admin")}}},
					BackendRefs: []gwapiv1.HTTPBackendRef{{BackendRef: gwapiv1.BackendRef{BackendObjectReference: gwapiv1.BackendObjectReference{Name: "admin-svc", Port: &port8080}}}},
				},
			},
		},
	}
	if err := kc.Create(ctx, hr); err != nil {
		t.Fatalf("create: %v", err)
	}
	defer kc.Delete(ctx, hr)

	vrataClient := vrata.NewClient("http://localhost:8080")
	rec := reconciler.NewReconciler(vrataClient, testLogger())
	rec.Init(ctx)
	if _, err := rec.ApplyHTTPRoute(ctx, mapper.MapHTTPRoute(gatewayHTTPRouteToInput(hr))); err != nil {
		t.Fatal(err)
	}

	routes, _ := vrataClient.ListRoutes(ctx)
	rule0Found, rule1Found := false, false
	for _, r := range routes {
		if r.Name == "k8s:default/e2e-multi-rule/rule-0/match-0" {
			if r.Match["pathPrefix"] != "/api" {
				t.Errorf("rule-0 path: %v", r.Match)
			}
			rule0Found = true
		}
		if r.Name == "k8s:default/e2e-multi-rule/rule-1/match-0" {
			if r.Match["pathPrefix"] != "/admin" {
				t.Errorf("rule-1 path: %v", r.Match)
			}
			rule1Found = true
		}
	}
	if !rule0Found || !rule1Found {
		t.Error("expected routes for both rules")
	}

	groups, _ := vrataClient.ListGroups(ctx)
	for _, g := range groups {
		if g.Name == "k8s:default/e2e-multi-rule" {
			if len(g.RouteIDs) != 2 {
				t.Errorf("expected 2 route IDs in group, got %d", len(g.RouteIDs))
			}
			return
		}
	}
	t.Error("group not found")
}
