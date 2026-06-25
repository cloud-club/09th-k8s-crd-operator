package main

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"

	deployv1alpha1 "github.com/cloud-club/09th-k8s-crd-operator/projects/release-operator/api/v1alpha1"
)

//go:embed static/index.html
var indexHTML embed.FS

const (
	defaultNamespace      = "team-5"
	defaultCRName         = "demo-app"
	defaultService        = "demo-app"
	defaultServicePort    = 8080
	defaultStableDeploy   = "demo-app"
	defaultCanaryDeploy   = "demo-app-canary"
	defaultCanaryImage    = "jangwoojung/canary-demo-app:canary"
	brokenImage           = "nginx:broken-demo-tag"
	canaryReleaseLabelKey = "canary-release"
)

type server struct {
	k8s       client.Client
	namespace string
	crName    string
	service   string
	port      int
}

func main() {
	s, err := newServer()
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/status", s.handleStatus)
	mux.HandleFunc("/api/traffic", s.handleTraffic)
	mux.HandleFunc("/api/start", s.handleStart)
	mux.HandleFunc("/api/reset", s.handleReset)
	mux.HandleFunc("/api/inject-crash", s.handleInjectCrash)
	mux.HandleFunc("/api/inject-imagepull", s.handleInjectImagePull)

	port := envOr("PORT", "8080")
	log.Printf("canary-demo-dashboard listening on :%s namespace=%s", port, s.namespace)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatal(err)
	}
}

func newServer() (*server, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, err
	}
	scheme := runtime.NewScheme()
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(deployv1alpha1.AddToScheme(scheme))
	k8s, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		return nil, err
	}
	port := defaultServicePort
	if v := os.Getenv("SERVICE_PORT"); v != "" {
		if p, err := strconv.Atoi(v); err == nil {
			port = p
		}
	}
	return &server{
		k8s:       k8s,
		namespace: envOr("NAMESPACE", defaultNamespace),
		crName:    envOr("CANARY_RELEASE_NAME", defaultCRName),
		service:   envOr("SERVICE_NAME", defaultService),
		port:      port,
	}, nil
}

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := indexHTML.ReadFile("static/index.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(data)
}

type statusResponse struct {
	CanaryRelease  crStatus          `json:"canaryRelease"`
	StableReplicas int32             `json:"stableReplicas"`
	CanaryReplicas int32             `json:"canaryReplicas"`
	Timers         timers            `json:"timers"`
	HealthCheck    *healthCheckInfo  `json:"healthCheck,omitempty"`
}

type healthCheckInfo struct {
	CheckIntervalSeconds int32 `json:"checkIntervalSeconds"`
	FailureThreshold     int32 `json:"failureThreshold"`
	PodRestartThreshold  int32 `json:"podRestartThreshold"`
}

type crStatus struct {
	Exists         bool   `json:"exists"`
	Phase          string `json:"phase,omitempty"`
	StepIndex      int32  `json:"stepIndex"`
	StepCount      int    `json:"stepCount"`
	Weight         int32  `json:"weight"`
	StableReplicas int32  `json:"stableReplicas"`
	CanaryReplicas int32  `json:"canaryReplicas"`
	TotalReplicas  int32  `json:"totalReplicas"`
	Message        string `json:"message,omitempty"`
	Image          string `json:"image,omitempty"`
}

type timers struct {
	TotalElapsedSec float64  `json:"totalElapsedSec"`
	StepElapsedSec  float64  `json:"stepElapsedSec"`
	NextStepInSec   *float64 `json:"nextStepInSec"`
	StepFrozen      bool     `json:"stepFrozen"`
}

func (s *server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	ctx := r.Context()
	resp := statusResponse{CanaryRelease: crStatus{Exists: false}}

	stableReplicas, _ := s.deploymentReplicas(ctx, defaultStableDeploy)
	canaryReplicas, _ := s.deploymentReplicas(ctx, defaultCanaryDeploy)
	resp.StableReplicas = stableReplicas
	resp.CanaryReplicas = canaryReplicas

	cr, err := s.getCR(ctx)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			writeError(w, err)
			return
		}
		writeJSON(w, resp)
		return
	}

	resp.CanaryRelease = crStatus{
		Exists:         true,
		Phase:          string(cr.Status.Phase),
		StepIndex:      cr.Status.CurrentStepIndex,
		StepCount:      len(cr.Spec.Steps),
		Weight:         cr.Status.CurrentWeight,
		StableReplicas: cr.Status.StableReplicas,
		CanaryReplicas: cr.Status.CanaryReplicas,
		TotalReplicas:  cr.Spec.TotalReplicas,
		Message:        cr.Status.Message,
		Image:          cr.Spec.Image,
	}
	resp.Timers = computeTimers(cr)
	resp.HealthCheck = &healthCheckInfo{
		CheckIntervalSeconds: cr.Spec.HealthCheck.CheckIntervalSeconds,
		FailureThreshold:     cr.Spec.HealthCheck.FailureThreshold,
		PodRestartThreshold:  cr.Spec.HealthCheck.PodRestartThreshold,
	}
	writeJSON(w, resp)
}

const (
	conditionPromoted = "Promoted"
	conditionDegraded = "Degraded"
)

func computeTimers(cr *deployv1alpha1.CanaryRelease) timers {
	now := time.Now()
	t := timers{}
	if !cr.CreationTimestamp.IsZero() {
		t.TotalElapsedSec = now.Sub(cr.CreationTimestamp.Time).Seconds()
	}

	stepEnd := now
	if isTerminalPhase(cr.Status.Phase) {
		t.StepFrozen = true
		if end, ok := phaseEndTime(cr); ok {
			stepEnd = end
		}
	}
	if cr.Status.LastStepTime != nil {
		elapsed := stepEnd.Sub(cr.Status.LastStepTime.Time).Seconds()
		if elapsed < 0 {
			elapsed = 0
		}
		t.StepElapsedSec = elapsed
	}

	if cr.Spec.AutoPromotion && cr.Status.Phase == deployv1alpha1.PhaseProgressing {
		interval := cr.Spec.Interval.Duration
		if interval > 0 && cr.Status.LastStepTime != nil {
			remaining := interval - now.Sub(cr.Status.LastStepTime.Time)
			if remaining < 0 {
				remaining = 0
			}
			sec := remaining.Seconds()
			t.NextStepInSec = &sec
		}
	}
	return t
}

func isTerminalPhase(phase deployv1alpha1.CanaryPhase) bool {
	switch phase {
	case deployv1alpha1.PhasePromoted, deployv1alpha1.PhaseRolledBack, deployv1alpha1.PhaseDegraded:
		return true
	default:
		return false
	}
}

func phaseEndTime(cr *deployv1alpha1.CanaryRelease) (time.Time, bool) {
	switch cr.Status.Phase {
	case deployv1alpha1.PhasePromoted:
		if c := meta.FindStatusCondition(cr.Status.Conditions, conditionPromoted); c != nil && c.Status == metav1.ConditionTrue {
			return c.LastTransitionTime.Time, true
		}
	case deployv1alpha1.PhaseRolledBack, deployv1alpha1.PhaseDegraded:
		if c := meta.FindStatusCondition(cr.Status.Conditions, conditionDegraded); c != nil && c.Status == metav1.ConditionTrue {
			return c.LastTransitionTime.Time, true
		}
	}
	return time.Time{}, false
}

func (s *server) handleTraffic(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w)
		return
	}
	url := fmt.Sprintf("http://%s.%s.svc.cluster.local:%d/api/whoami", s.service, s.namespace, s.port)
	client := &http.Client{Timeout: 2 * time.Second}
	stable, canary := 0, 0
	for i := 0; i < 50; i++ {
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, url, nil)
		if err != nil {
			break
		}
		req.Close = true
		resp, err := client.Do(req)
		if err != nil {
			continue
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		var who map[string]string
		if json.Unmarshal(body, &who) != nil {
			continue
		}
		switch who["version"] {
		case "Canary v2":
			canary++
		case "Stable v1":
			stable++
		}
	}
	writeJSON(w, map[string]int{"stable": stable, "canary": canary})
}

func (s *server) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx := r.Context()
	cr, err := s.getCR(ctx)
	if err == nil {
		phase := string(cr.Status.Phase)
		if phase == "" {
			phase = "unknown"
		}
		writeError(w, fmt.Errorf("이전 CanaryRelease가 남아 있습니다 (Phase: %s). 「리셋」 후 다시 시작하세요", phase))
		return
	}
	if !apierrors.IsNotFound(err) {
		writeError(w, err)
		return
	}
	cr = s.defaultCR()
	if err := s.k8s.Create(ctx, cr); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "created"})
}

func (s *server) handleReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx := r.Context()
	cr := &deployv1alpha1.CanaryRelease{}
	err := s.k8s.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: s.crName}, cr)
	if err == nil {
		if err := s.k8s.Delete(ctx, cr); err != nil && !apierrors.IsNotFound(err) {
			writeError(w, err)
			return
		}
	}
	// Promoted 등으로 stable이 0이어도 반드시 복구 (canary 삭제 실패와 무관)
	if err := s.scaleDeployment(ctx, defaultStableDeploy, 10); err != nil {
		writeError(w, err)
		return
	}
	// Operator finalizer가 정리하지만, 남은 canary Deployment orphan 제거
	canary := &appsv1.Deployment{}
	err = s.k8s.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: defaultCanaryDeploy}, canary)
	if err == nil {
		if err := s.k8s.Delete(ctx, canary); err != nil && !apierrors.IsNotFound(err) {
			writeError(w, err)
			return
		}
	}
	writeJSON(w, map[string]string{"status": "reset"})
}

func (s *server) handleInjectCrash(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx := r.Context()
	var pods corev1.PodList
	if err := s.k8s.List(ctx, &pods,
		client.InNamespace(s.namespace),
		client.MatchingLabels{canaryReleaseLabelKey: s.crName},
	); err != nil {
		writeError(w, err)
		return
	}
	var target *corev1.Pod
	for i := range pods.Items {
		p := &pods.Items[i]
		if p.Status.PodIP != "" && p.Status.Phase == corev1.PodRunning {
			target = p
			break
		}
	}
	if target == nil {
		writeError(w, errors.New("no running canary pod found"))
		return
	}
	url := fmt.Sprintf("http://%s:8080/admin/crash", target.Status.PodIP)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, nil)
	if err != nil {
		writeError(w, err)
		return
	}
	resp, err := (&http.Client{Timeout: 3 * time.Second}).Do(req)
	if err != nil {
		// 프로세스 즉시 종료 시 연결 끊김(EOF)은 정상적인 크래시 주입 결과
		if isExpectedCrashDisconnect(err) {
			writeJSON(w, map[string]string{"status": "injected", "pod": target.Name})
			return
		}
		writeError(w, fmt.Errorf("crash request failed: %w", err))
		return
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)
	writeJSON(w, map[string]string{"status": "injected", "pod": target.Name})
}

func isExpectedCrashDisconnect(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, io.EOF) {
		return true
	}
	msg := err.Error()
	return strings.Contains(msg, "EOF") || strings.Contains(msg, "connection reset by peer")
}

func (s *server) handleInjectImagePull(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w)
		return
	}
	ctx := r.Context()
	cr, err := s.getCR(ctx)
	if err != nil {
		writeError(w, err)
		return
	}
	patch := []byte(fmt.Sprintf(`{"spec":{"image":%q}}`, brokenImage))
	if err := s.k8s.Patch(ctx, cr, client.RawPatch(types.MergePatchType, patch)); err != nil {
		writeError(w, err)
		return
	}
	writeJSON(w, map[string]string{"status": "patched", "image": brokenImage})
}

func (s *server) defaultCR() *deployv1alpha1.CanaryRelease {
	return &deployv1alpha1.CanaryRelease{
		ObjectMeta: metav1.ObjectMeta{
			Name:      s.crName,
			Namespace: s.namespace,
			Labels: map[string]string{
				"app.kubernetes.io/name":       "release-operator",
				"app.kubernetes.io/managed-by": "demo-dashboard",
			},
		},
		Spec: deployv1alpha1.CanaryReleaseSpec{
			StableRef: deployv1alpha1.ResourceRef{
				APIVersion: "apps/v1",
				Kind:       "Deployment",
				Name:       defaultStableDeploy,
			},
			ServiceRef: deployv1alpha1.ServiceRef{Name: defaultService},
			Image:      defaultCanaryImage,
			Port:       8080,
			TotalReplicas: 10,
			Steps: []deployv1alpha1.CanaryStep{
				{Weight: 10}, {Weight: 30}, {Weight: 60}, {Weight: 100},
			},
			Interval:      metav1.Duration{Duration: 30 * time.Second},
			AutoPromotion: true,
			HealthCheck: deployv1alpha1.HealthCheckSpec{
				CheckIntervalSeconds: 10,
				FailureThreshold:     3,
				MaxUnavailableCanary: 0,
				PodRestartThreshold:  1,
			},
			FailurePolicy: deployv1alpha1.FailurePolicySpec{
				Action:                 deployv1alpha1.FailureActionRollback,
				RestoreStableReplicas:  true,
				ScaleCanaryToZero:      true,
				DeleteCanaryOnRollback: false,
				DeleteCanaryOnDelete:   true,
			},
		},
	}
}

func (s *server) getCR(ctx context.Context) (*deployv1alpha1.CanaryRelease, error) {
	cr := &deployv1alpha1.CanaryRelease{}
	err := s.k8s.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: s.crName}, cr)
	return cr, err
}

func (s *server) deploymentReplicas(ctx context.Context, name string) (int32, error) {
	dep := &appsv1.Deployment{}
	err := s.k8s.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: name}, dep)
	if apierrors.IsNotFound(err) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	if dep.Spec.Replicas == nil {
		return 0, nil
	}
	return *dep.Spec.Replicas, nil
}

func (s *server) scaleDeployment(ctx context.Context, name string, replicas int32) error {
	dep := &appsv1.Deployment{}
	if err := s.k8s.Get(ctx, types.NamespacedName{Namespace: s.namespace, Name: name}, dep); err != nil {
		return err
	}
	dep.Spec.Replicas = &replicas
	return s.k8s.Update(ctx, dep)
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}

func methodNotAllowed(w http.ResponseWriter) {
	http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

