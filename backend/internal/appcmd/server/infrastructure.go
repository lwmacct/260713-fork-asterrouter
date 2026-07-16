package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/astercloud/asterrouter/backend/internal/config"
	"github.com/astercloud/asterrouter/backend/internal/controlplane"
	"github.com/redis/go-redis/v9"
)

func configureArtifactStore(ctx context.Context, cfg config.Artifacts, service *controlplane.Service) error {
	switch cfg.Driver {
	case "none":
		return nil
	case "local":
		store, err := controlplane.NewLocalArtifactStore(cfg.LocalRoot)
		if err != nil {
			return fmt.Errorf("initialize local artifact store: %w", err)
		}
		if err = service.SetArtifactStore(store); err != nil {
			return fmt.Errorf("configure local artifact store: %w", err)
		}
		return nil
	case "s3":
		store, err := controlplane.NewS3ArtifactStore(ctx, controlplane.S3ArtifactStoreConfig{
			Endpoint: cfg.S3.Endpoint, Region: cfg.S3.Region, Bucket: cfg.S3.Bucket,
			Prefix: cfg.S3.Prefix, AccessKey: cfg.S3.AccessKey, SecretKey: cfg.S3.SecretKey, PathStyle: cfg.S3.PathStyle,
		})
		if err != nil {
			return fmt.Errorf("initialize S3 artifact store: %w", err)
		}
		if err = service.SetArtifactStore(store); err != nil {
			return fmt.Errorf("configure S3 artifact store: %w", err)
		}
		return nil
	default:
		return fmt.Errorf("unsupported artifact store driver %q", cfg.Driver)
	}
}

func configureAIJobInfrastructure(ctx context.Context, jobs config.Jobs, redisConfig config.Redis, service *controlplane.Service) (controlplane.AIJobDeliveryQueue, func(), error) {
	queueDriver := strings.TrimSpace(jobs.Queue.Driver)
	affinityDriver := strings.TrimSpace(jobs.RoutingAffinityDriver)
	if queueDriver != "redis" && affinityDriver != "redis" {
		queue, err := controlplane.NewMemoryAIJobDeliveryQueue(30 * time.Second)
		if err != nil {
			return nil, func() {}, err
		}
		service.SetAIJobReadyIndex(controlplane.NewMemoryAIJobReadyIndex())
		return queue, func() {}, nil
	}
	options, err := redis.ParseURL(strings.TrimSpace(redisConfig.URL))
	if err != nil {
		return nil, func() {}, fmt.Errorf("parse Redis URL: %w", err)
	}
	client := redis.NewClient(options)
	closeClient := func() { _ = client.Close() }
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err = client.Ping(pingCtx).Err(); err != nil {
		closeClient()
		return nil, func() {}, fmt.Errorf("connect to Redis: %w", err)
	}
	namespace := strings.TrimSpace(redisConfig.Namespace)
	var queue controlplane.AIJobDeliveryQueue
	if queueDriver == "redis" {
		queue, err = controlplane.NewRedisAIJobDeliveryQueue(client, controlplane.RedisAIJobDeliveryQueueConfig{Namespace: namespace})
		if err != nil {
			closeClient()
			return nil, func() {}, err
		}
		readyIndex, readyErr := controlplane.NewRedisAIJobReadyIndex(client, controlplane.RedisAIJobReadyIndexConfig{Namespace: namespace})
		if readyErr != nil {
			closeClient()
			return nil, func() {}, readyErr
		}
		capacityStore, capacityErr := controlplane.NewRedisProviderCapacityStore(client, controlplane.RedisProviderCapacityStoreConfig{Namespace: namespace})
		if capacityErr != nil {
			closeClient()
			return nil, func() {}, capacityErr
		}
		service.SetAIJobReadyIndex(readyIndex)
		service.SetProviderCapacityStore(capacityStore)
	} else {
		queue, err = controlplane.NewMemoryAIJobDeliveryQueue(30 * time.Second)
		if err != nil {
			closeClient()
			return nil, func() {}, err
		}
		service.SetAIJobReadyIndex(controlplane.NewMemoryAIJobReadyIndex())
	}
	if affinityDriver == "redis" {
		coordinator, coordinatorErr := controlplane.NewRedisRoutingAffinityCoordinator(client, controlplane.RedisRoutingAffinityCoordinatorConfig{Namespace: namespace})
		if coordinatorErr != nil {
			closeClient()
			return nil, func() {}, coordinatorErr
		}
		service.SetRoutingAffinityCoordinator(coordinator)
	}
	return queue, closeClient, nil
}
