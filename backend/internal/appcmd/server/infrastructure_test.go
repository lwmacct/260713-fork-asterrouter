package server

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/redis/go-redis/v9"
)

func TestConfigureAIJobInfrastructureSupportsRedisAffinityWithMemoryQueue(t *testing.T) {
	rawURL := strings.TrimSpace(os.Getenv("ASTER_TEST_REDIS_URL"))
	if rawURL == "" {
		t.Skip("ASTER_TEST_REDIS_URL is not set")
	}
	namespace := fmt.Sprintf("affinity-wiring-%d", time.Now().UnixNano())
	service := controlplane.NewService(controlplane.NewMemoryRepository(), "/v1", "affinity-wiring-secret")
	queue, closeInfrastructure, err := configureAIJobInfrastructure(t.Context(), config.Jobs{
		Queue: config.JobQueue{Driver: "memory"}, RoutingAffinityDriver: "redis",
	}, config.Redis{URL: rawURL, Namespace: namespace}, service)
	if err != nil {
		t.Fatalf("configureAIJobInfrastructure(): %v", err)
	}
	defer closeInfrastructure()
	if _, ok := queue.(*controlplane.MemoryAIJobDeliveryQueue); !ok {
		t.Fatalf("queue type=%T, want memory queue", queue)
	}
	input := controlplane.GatewayAffinityInput{
		TenantID: "tenant", PrincipalID: "principal", CredentialID: "credential", Model: "model",
		Protocol: "openai_chat_completions", RouteGroup: "default", PolicyVersion: 1,
	}
	if err = service.BindGatewayCandidateAffinity(t.Context(), input, controlplane.GatewayProvider{ID: "provider-a"}); err != nil {
		t.Fatal(err)
	}
	options, err := redis.ParseURL(rawURL)
	if err != nil {
		t.Fatal(err)
	}
	client := redis.NewClient(options)
	t.Cleanup(func() { _ = client.Close() })
	pattern := "asterrouter:{" + namespace + ":routing_affinity}:*"
	keys, err := client.Keys(t.Context(), pattern).Result()
	if err != nil || len(keys) != 1 {
		t.Fatalf("routing affinity keys=%v err=%v", keys, err)
	}
	t.Cleanup(func() {
		cleanupCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = client.Del(cleanupCtx, keys...).Err()
	})
}
