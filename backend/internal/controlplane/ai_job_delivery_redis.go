package controlplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"
)

var (
	ErrRedisAIJobDeliveryConfig         = errors.New("invalid redis ai job delivery queue configuration")
	ErrAIJobDeliveryEnvelopeTooLarge    = errors.New("ai job delivery envelope exceeds the configured payload limit")
	ErrAIJobDeliveryInfrastructureState = errors.New("ai job delivery queue infrastructure state is invalid")
)

const (
	redisAIJobDeliveryDefaultNamespace      = "asterrouter"
	redisAIJobDeliveryDefaultConsumerGroup  = "asterrouter-workers"
	redisAIJobDeliveryDefaultLease          = 30 * time.Second
	redisAIJobDeliveryDefaultDedupeTTL      = 7 * 24 * time.Hour
	redisAIJobDeliveryDefaultMaxPayload     = 16 * 1024
	redisAIJobDeliveryDefaultPromotionBatch = int64(100)
	redisAIJobDeliveryMaxReasonBytes        = 2048
)

type RedisAIJobDeliveryQueueConfig struct {
	Namespace       string
	ConsumerGroup   string
	DeliveryLease   time.Duration
	DedupeTTL       time.Duration
	MaxPayloadBytes int
	PromotionBatch  int64
}

type RedisAIJobDeliveryQueue struct {
	client         redis.UniversalClient
	consumerGroup  string
	deliveryLease  time.Duration
	dedupeTTL      time.Duration
	maxPayload     int
	promotionBatch int64
	streamKey      string
	delayedKey     string
	leaseTokenKey  string
	leaseUntilKey  string
	attemptKey     string
	deadLetterKey  string
	dedupePrefix   string
}

func NewRedisAIJobDeliveryQueue(client redis.UniversalClient, config RedisAIJobDeliveryQueueConfig) (*RedisAIJobDeliveryQueue, error) {
	if client == nil {
		return nil, ErrRedisAIJobDeliveryConfig
	}
	namespace := strings.TrimSpace(config.Namespace)
	if namespace == "" {
		namespace = redisAIJobDeliveryDefaultNamespace
	}
	if !validRedisAIJobDeliveryName(namespace) {
		return nil, fmt.Errorf("%w: invalid namespace", ErrRedisAIJobDeliveryConfig)
	}
	consumerGroup := strings.TrimSpace(config.ConsumerGroup)
	if consumerGroup == "" {
		consumerGroup = redisAIJobDeliveryDefaultConsumerGroup
	}
	if !validRedisAIJobDeliveryName(consumerGroup) {
		return nil, fmt.Errorf("%w: invalid consumer group", ErrRedisAIJobDeliveryConfig)
	}
	deliveryLease := config.DeliveryLease
	if deliveryLease == 0 {
		deliveryLease = redisAIJobDeliveryDefaultLease
	}
	dedupeTTL := config.DedupeTTL
	if dedupeTTL == 0 {
		dedupeTTL = redisAIJobDeliveryDefaultDedupeTTL
	}
	maxPayload := config.MaxPayloadBytes
	if maxPayload == 0 {
		maxPayload = redisAIJobDeliveryDefaultMaxPayload
	}
	promotionBatch := config.PromotionBatch
	if promotionBatch == 0 {
		promotionBatch = redisAIJobDeliveryDefaultPromotionBatch
	}
	if deliveryLease <= 0 || dedupeTTL <= 0 || maxPayload <= 0 || promotionBatch <= 0 {
		return nil, ErrRedisAIJobDeliveryConfig
	}
	prefix := "asterrouter:{" + namespace + ":ai_job_delivery}"
	return &RedisAIJobDeliveryQueue{
		client: client, consumerGroup: consumerGroup, deliveryLease: deliveryLease, dedupeTTL: dedupeTTL,
		maxPayload: maxPayload, promotionBatch: promotionBatch,
		streamKey: prefix + ":stream", delayedKey: prefix + ":delayed",
		leaseTokenKey: prefix + ":lease_tokens", leaseUntilKey: prefix + ":lease_until",
		attemptKey: prefix + ":attempts", deadLetterKey: prefix + ":dead_letters", dedupePrefix: prefix + ":dedupe:",
	}, nil
}

func (q *RedisAIJobDeliveryQueue) Publish(ctx context.Context, envelope AIJobDeliveryEnvelope, dedupeKey string, availableAt time.Time) error {
	payload, err := q.marshalEnvelope(envelope)
	if err != nil {
		return err
	}
	dedupeKey = strings.TrimSpace(dedupeKey)
	if dedupeKey == "" {
		dedupeKey = envelope.DedupeKey()
	}
	if dedupeKey == "" {
		return ErrAIJobDeliveryEnvelopeInvalid
	}
	now := time.Now().UTC()
	if availableAt.IsZero() {
		availableAt = now
	}
	delayed, err := json.Marshal(redisDelayedAIJobDelivery{Envelope: string(payload)})
	if err != nil {
		return err
	}
	result, err := redisAIJobDeliveryPublishScript.Run(ctx, q.client,
		[]string{q.streamKey, q.delayedKey, q.dedupeKey(dedupeKey)},
		string(payload), q.dedupeTTL.Milliseconds(), availableAt.UnixMilli(), now.UnixMilli(), string(delayed),
	).Int64()
	if err != nil {
		return err
	}
	switch result {
	case 1, 0:
		return nil
	case -1:
		return ErrAIJobDeliveryDedupeConflict
	default:
		return ErrAIJobDeliveryInfrastructureState
	}
}

func (q *RedisAIJobDeliveryQueue) Receive(ctx context.Context, consumer string, maxItems int, wait time.Duration) ([]AIJobDelivery, error) {
	consumer = strings.TrimSpace(consumer)
	if consumer == "" || maxItems <= 0 {
		return []AIJobDelivery{}, nil
	}
	if wait < 0 {
		wait = 0
	}
	if err := q.ensureConsumerGroup(ctx); err != nil {
		return nil, err
	}
	deadline := time.Time{}
	if wait > 0 {
		deadline = time.Now().Add(wait)
	}
	for {
		if err := q.promoteDue(ctx, time.Now().UTC()); err != nil {
			return nil, err
		}
		reclaimed, err := q.reclaimExpired(ctx, consumer, maxItems)
		if err != nil {
			return nil, err
		}
		if len(reclaimed) > 0 {
			return reclaimed, nil
		}
		block, done, err := q.receiveBlock(ctx, deadline)
		if err != nil {
			return nil, err
		}
		if done {
			return []AIJobDelivery{}, nil
		}
		streams, err := q.client.XReadGroup(ctx, &redis.XReadGroupArgs{
			Group: q.consumerGroup, Consumer: consumer, Streams: []string{q.streamKey, ">"}, Count: int64(maxItems), Block: block,
		}).Result()
		if errors.Is(err, redis.Nil) {
			if deadline.IsZero() {
				return []AIJobDelivery{}, nil
			}
			continue
		}
		if err != nil {
			if redisAIJobDeliveryNoGroup(err) {
				if groupErr := q.ensureConsumerGroup(ctx); groupErr == nil {
					continue
				}
			}
			return nil, err
		}
		messages := flattenRedisAIJobMessages(streams)
		if len(messages) == 0 {
			if deadline.IsZero() {
				return []AIJobDelivery{}, nil
			}
			continue
		}
		deliveries, err := q.establishNewLeases(ctx, consumer, messages)
		if err != nil {
			return nil, err
		}
		if len(deliveries) > 0 {
			return deliveries, nil
		}
	}
}

func (q *RedisAIJobDeliveryQueue) Extend(ctx context.Context, delivery AIJobDelivery, leaseUntil time.Time) error {
	now := time.Now().UTC()
	if !leaseUntil.After(now) {
		return ErrAIJobDeliveryLeaseExpired
	}
	result, err := redisAIJobDeliveryExtendScript.Run(ctx, q.client,
		[]string{q.streamKey, q.leaseTokenKey, q.leaseUntilKey},
		q.consumerGroup, delivery.Consumer, delivery.ID, delivery.LeaseToken, now.UnixMilli(), leaseUntil.UnixMilli(),
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) Ack(ctx context.Context, delivery AIJobDelivery) error {
	result, err := redisAIJobDeliveryAckScript.Run(ctx, q.client,
		[]string{q.streamKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, delivery.ID, delivery.LeaseToken, time.Now().UTC().UnixMilli(),
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) Nack(ctx context.Context, delivery AIJobDelivery, retryAt time.Time, reason string) error {
	payload, err := q.marshalEnvelope(delivery.Envelope)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	if retryAt.IsZero() || retryAt.Before(now) {
		retryAt = now
	}
	reason = trimRedisAIJobDeliveryReason(reason)
	delayed, err := json.Marshal(redisDelayedAIJobDelivery{Envelope: string(payload), AttemptBase: delivery.Attempt, LastError: reason})
	if err != nil {
		return err
	}
	result, err := redisAIJobDeliveryNackScript.Run(ctx, q.client,
		[]string{q.streamKey, q.delayedKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, delivery.ID, delivery.LeaseToken, now.UnixMilli(), retryAt.UnixMilli(),
		string(payload), delivery.Attempt, reason, string(delayed),
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) DeadLetter(ctx context.Context, delivery AIJobDelivery, reason string) error {
	payload, err := q.marshalEnvelope(delivery.Envelope)
	if err != nil {
		return err
	}
	result, err := redisAIJobDeliveryDeadLetterScript.Run(ctx, q.client,
		[]string{q.streamKey, q.deadLetterKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, delivery.ID, delivery.LeaseToken, time.Now().UTC().UnixMilli(),
		string(payload), delivery.Attempt, trimRedisAIJobDeliveryReason(reason),
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) ensureConsumerGroup(ctx context.Context) error {
	err := q.client.XGroupCreateMkStream(ctx, q.streamKey, q.consumerGroup, "0").Err()
	if err == nil || strings.Contains(err.Error(), "BUSYGROUP") {
		return nil
	}
	return err
}

func (q *RedisAIJobDeliveryQueue) promoteDue(ctx context.Context, now time.Time) error {
	_, err := redisAIJobDeliveryPromoteScript.Run(ctx, q.client,
		[]string{q.streamKey, q.delayedKey}, now.UnixMilli(), q.promotionBatch,
	).Result()
	return err
}

func (q *RedisAIJobDeliveryQueue) reclaimExpired(ctx context.Context, consumer string, maxItems int) ([]AIJobDelivery, error) {
	now := time.Now().UTC()
	leaseUntil := now.Add(q.deliveryLease)
	scanCount := maxItems * 10
	if scanCount < 100 {
		scanCount = 100
	}
	values, err := redisAIJobDeliveryReclaimScript.Run(ctx, q.client,
		[]string{q.streamKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, consumer, now.UnixMilli(), maxItems, leaseUntil.UnixMilli(), "delivery_lease_"+randomID(16),
		(q.deliveryLease * 2).Milliseconds(), scanCount,
	).StringSlice()
	if err != nil {
		return nil, err
	}
	return q.decodeClaimedDeliveries(ctx, consumer, leaseUntil, values)
}

func (q *RedisAIJobDeliveryQueue) establishNewLeases(ctx context.Context, consumer string, messages []redis.XMessage) ([]AIJobDelivery, error) {
	leaseUntil := time.Now().UTC().Add(q.deliveryLease)
	args := make([]any, 0, 4+2*len(messages))
	args = append(args, q.consumerGroup, consumer, leaseUntil.UnixMilli(), "delivery_lease_"+randomID(16))
	byID := make(map[string]redis.XMessage, len(messages))
	for _, message := range messages {
		byID[message.ID] = message
		args = append(args, message.ID, redisAIJobDeliveryAttemptBase(message.Values))
	}
	values, err := redisAIJobDeliveryEstablishScript.Run(ctx, q.client,
		[]string{q.streamKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey}, args...,
	).StringSlice()
	if err != nil {
		return nil, err
	}
	deliveries := make([]AIJobDelivery, 0, len(values))
	for _, raw := range values {
		var lease redisAIJobDeliveryLeaseResultValue
		if err := json.Unmarshal([]byte(raw), &lease); err != nil {
			return nil, err
		}
		message, found := byID[lease.ID]
		if !found {
			return nil, ErrAIJobDeliveryInfrastructureState
		}
		delivery, err := q.deliveryFromValues(lease.ID, consumer, lease.Token, lease.Attempt, leaseUntil, message.Values)
		if err != nil {
			if rejectErr := q.deadLetterMalformed(ctx, lease.ID, lease.Token, lease.Attempt, redisAIJobDeliveryString(message.Values["envelope"]), err); rejectErr != nil {
				return nil, errors.Join(err, rejectErr)
			}
			continue
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (q *RedisAIJobDeliveryQueue) decodeClaimedDeliveries(ctx context.Context, consumer string, leaseUntil time.Time, values []string) ([]AIJobDelivery, error) {
	deliveries := make([]AIJobDelivery, 0, len(values))
	for _, raw := range values {
		var claimed redisAIJobDeliveryClaimedValue
		if err := json.Unmarshal([]byte(raw), &claimed); err != nil {
			return nil, err
		}
		values := make(map[string]any, len(claimed.Fields))
		for key, value := range claimed.Fields {
			values[key] = value
		}
		delivery, err := q.deliveryFromValues(claimed.ID, consumer, claimed.Token, claimed.Attempt, leaseUntil, values)
		if err != nil {
			if rejectErr := q.deadLetterMalformed(ctx, claimed.ID, claimed.Token, claimed.Attempt, claimed.Fields["envelope"], err); rejectErr != nil {
				return nil, errors.Join(err, rejectErr)
			}
			continue
		}
		deliveries = append(deliveries, delivery)
	}
	return deliveries, nil
}

func (q *RedisAIJobDeliveryQueue) deliveryFromValues(id, consumer, leaseToken string, attempt int, leaseUntil time.Time, values map[string]any) (AIJobDelivery, error) {
	var envelope AIJobDeliveryEnvelope
	if err := json.Unmarshal([]byte(redisAIJobDeliveryString(values["envelope"])), &envelope); err != nil {
		return AIJobDelivery{}, err
	}
	if err := validateAIJobDeliveryEnvelope(envelope); err != nil {
		return AIJobDelivery{}, err
	}
	return AIJobDelivery{ID: id, Envelope: envelope, Consumer: consumer, Attempt: attempt, LeaseUntil: leaseUntil, LeaseToken: leaseToken}, nil
}

func (q *RedisAIJobDeliveryQueue) deadLetterMalformed(ctx context.Context, id, leaseToken string, attempt int, payload string, cause error) error {
	result, err := redisAIJobDeliveryDeadLetterScript.Run(ctx, q.client,
		[]string{q.streamKey, q.deadLetterKey, q.leaseTokenKey, q.leaseUntilKey, q.attemptKey},
		q.consumerGroup, id, leaseToken, time.Now().UTC().UnixMilli(), payload, attempt, trimRedisAIJobDeliveryReason(cause.Error()),
	).Int64()
	return redisAIJobDeliveryLeaseResult(result, err)
}

func (q *RedisAIJobDeliveryQueue) receiveBlock(ctx context.Context, deadline time.Time) (time.Duration, bool, error) {
	if deadline.IsZero() {
		return -1, false, nil
	}
	remaining := time.Until(deadline)
	if remaining <= 0 {
		return 0, true, nil
	}
	pipe := q.client.Pipeline()
	nextDelayed := pipe.ZRangeWithScores(ctx, q.delayedKey, 0, 0)
	nextLease := pipe.ZRangeWithScores(ctx, q.leaseUntilKey, 0, 0)
	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return 0, false, err
	}
	nextWake := time.Time{}
	for _, values := range [][]redis.Z{nextDelayed.Val(), nextLease.Val()} {
		if len(values) == 0 {
			continue
		}
		candidate := time.UnixMilli(int64(values[0].Score))
		if nextWake.IsZero() || candidate.Before(nextWake) {
			nextWake = candidate
		}
	}
	if !nextWake.IsZero() {
		untilNext := time.Until(nextWake)
		if untilNext < remaining {
			remaining = untilNext
		}
	}
	if q.deliveryLease < remaining {
		remaining = q.deliveryLease
	}
	if remaining < time.Millisecond {
		remaining = time.Millisecond
	}
	return remaining, false, nil
}

func (q *RedisAIJobDeliveryQueue) marshalEnvelope(envelope AIJobDeliveryEnvelope) ([]byte, error) {
	if err := validateAIJobDeliveryEnvelope(envelope); err != nil {
		return nil, err
	}
	payload, err := json.Marshal(envelope)
	if err != nil {
		return nil, err
	}
	if len(payload) > q.maxPayload {
		return nil, ErrAIJobDeliveryEnvelopeTooLarge
	}
	return payload, nil
}

func (q *RedisAIJobDeliveryQueue) dedupeKey(value string) string {
	return q.dedupePrefix + prefix(hashAPIKey(value), 40)
}

func redisAIJobDeliveryLeaseResult(result int64, err error) error {
	if err != nil {
		return err
	}
	switch result {
	case 1:
		return nil
	case 0:
		return ErrAIJobDeliveryLeaseConflict
	case -1:
		return ErrAIJobDeliveryLeaseExpired
	case -2:
		return ErrAIJobDeliveryNotFound
	default:
		return ErrAIJobDeliveryInfrastructureState
	}
}

func flattenRedisAIJobMessages(streams []redis.XStream) []redis.XMessage {
	var messages []redis.XMessage
	for _, stream := range streams {
		messages = append(messages, stream.Messages...)
	}
	return messages
}

func redisAIJobDeliveryAttemptBase(values map[string]any) int {
	value, err := strconv.Atoi(redisAIJobDeliveryString(values["attempt_base"]))
	if err != nil || value < 0 {
		return 0
	}
	return value
}

func redisAIJobDeliveryString(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case []byte:
		return string(typed)
	case nil:
		return ""
	default:
		return fmt.Sprint(typed)
	}
}

func redisAIJobDeliveryNoGroup(err error) bool {
	return err != nil && strings.Contains(err.Error(), "NOGROUP")
}

func validRedisAIJobDeliveryName(value string) bool {
	if value == "" || len(value) > 128 {
		return false
	}
	for _, character := range value {
		if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
			(character >= '0' && character <= '9') || strings.ContainsRune("._:-", character) {
			continue
		}
		return false
	}
	return true
}

func trimRedisAIJobDeliveryReason(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= redisAIJobDeliveryMaxReasonBytes {
		return value
	}
	return value[:redisAIJobDeliveryMaxReasonBytes]
}

type redisDelayedAIJobDelivery struct {
	Envelope    string `json:"envelope"`
	AttemptBase int    `json:"attempt_base"`
	LastError   string `json:"last_error,omitempty"`
}

type redisAIJobDeliveryLeaseResultValue struct {
	ID      string `json:"id"`
	Token   string `json:"token"`
	Attempt int    `json:"attempt"`
}

type redisAIJobDeliveryClaimedValue struct {
	ID      string            `json:"id"`
	Token   string            `json:"token"`
	Attempt int               `json:"attempt"`
	Fields  map[string]string `json:"fields"`
}

var redisAIJobDeliveryPublishScript = redis.NewScript(`
local existing = redis.call('GET', KEYS[3])
if existing then
  if existing == ARGV[1] then return 0 end
  return -1
end
redis.call('SET', KEYS[3], ARGV[1], 'PX', ARGV[2])
local published
if tonumber(ARGV[3]) <= tonumber(ARGV[4]) then
  published = redis.pcall('XADD', KEYS[1], '*', 'envelope', ARGV[1], 'attempt_base', '0')
else
  published = redis.pcall('ZADD', KEYS[2], ARGV[3], ARGV[5])
end
if type(published) == 'table' and published.err then
  redis.call('DEL', KEYS[3])
  return published
end
return 1
`)

var redisAIJobDeliveryPromoteScript = redis.NewScript(`
local members = redis.call('ZRANGEBYSCORE', KEYS[2], '-inf', ARGV[1], 'LIMIT', 0, ARGV[2])
for _, member in ipairs(members) do
  local data = cjson.decode(member)
  redis.call('XADD', KEYS[1], '*', 'envelope', data.envelope, 'attempt_base', tostring(data.attempt_base or 0), 'last_error', data.last_error or '')
  redis.call('ZREM', KEYS[2], member)
end
return #members
`)

var redisAIJobDeliveryReclaimScript = redis.NewScript(`
local output = {}
local claimedCount = 0
local function cleanup(id)
  redis.call('HDEL', KEYS[2], id)
  redis.call('ZREM', KEYS[3], id)
  redis.call('HDEL', KEYS[4], id)
end
local function claim(id, priorDeliveries)
  local claimed = redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, id)
  if #claimed == 0 then
    cleanup(id)
    return
  end
  local message = claimed[1]
  local fields = {}
  local attemptBase = 0
  for index = 1, #message[2], 2 do
    local key = message[2][index]
    local value = message[2][index + 1]
    fields[key] = value
    if key == 'attempt_base' then attemptBase = tonumber(value) or 0 end
  end
  local token = ARGV[6] .. ':' .. id
  redis.call('HSET', KEYS[2], id, token)
  redis.call('ZADD', KEYS[3], ARGV[5], id)
  if priorDeliveries and not redis.call('HGET', KEYS[4], id) then
    redis.call('HSET', KEYS[4], id, priorDeliveries)
  else
    redis.call('HSETNX', KEYS[4], id, attemptBase)
  end
  local attempt = redis.call('HINCRBY', KEYS[4], id, 1)
  table.insert(output, cjson.encode({id=id, token=token, attempt=attempt, fields=fields}))
  claimedCount = claimedCount + 1
end
local due = redis.call('ZRANGEBYSCORE', KEYS[3], '-inf', ARGV[3], 'LIMIT', 0, ARGV[4])
for _, id in ipairs(due) do
  claim(id, nil)
end
if claimedCount < tonumber(ARGV[4]) then
  local pending = redis.call('XPENDING', KEYS[1], ARGV[1], 'IDLE', ARGV[7], '-', '+', ARGV[8])
  for _, entry in ipairs(pending) do
    if claimedCount >= tonumber(ARGV[4]) then break end
    local id = entry[1]
    if not redis.call('ZSCORE', KEYS[3], id) then claim(id, tonumber(entry[4]) or 1) end
  end
end
return output
`)

var redisAIJobDeliveryEstablishScript = redis.NewScript(`
local output = {}
for index = 5, #ARGV, 2 do
  local id = ARGV[index]
  local claimed = redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, id, 'IDLE', 0, 'JUSTID')
  if #claimed > 0 then
    local token = ARGV[4] .. ':' .. id
    redis.call('HSET', KEYS[2], id, token)
    redis.call('ZADD', KEYS[3], ARGV[3], id)
    redis.call('HSETNX', KEYS[4], id, tonumber(ARGV[index + 1]) or 0)
    local attempt = redis.call('HINCRBY', KEYS[4], id, 1)
    table.insert(output, cjson.encode({id=id, token=token, attempt=attempt}))
  end
end
return output
`)

var redisAIJobDeliveryExtendScript = redis.NewScript(`
local token = redis.call('HGET', KEYS[2], ARGV[3])
if not token then return -2 end
if token ~= ARGV[4] then return 0 end
local leaseUntil = tonumber(redis.call('ZSCORE', KEYS[3], ARGV[3]) or '0')
if leaseUntil <= tonumber(ARGV[5]) then return -1 end
local claimed = redis.call('XCLAIM', KEYS[1], ARGV[1], ARGV[2], 0, ARGV[3], 'IDLE', 0, 'JUSTID')
if #claimed == 0 then
  redis.call('HDEL', KEYS[2], ARGV[3])
  redis.call('ZREM', KEYS[3], ARGV[3])
  return -2
end
redis.call('ZADD', KEYS[3], ARGV[6], ARGV[3])
return 1
`)

var redisAIJobDeliveryAckScript = redis.NewScript(`
local token = redis.call('HGET', KEYS[2], ARGV[2])
if not token then return -2 end
if token ~= ARGV[3] then return 0 end
local leaseUntil = tonumber(redis.call('ZSCORE', KEYS[3], ARGV[2]) or '0')
if leaseUntil <= tonumber(ARGV[4]) then return -1 end
local acknowledged = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
redis.call('HDEL', KEYS[2], ARGV[2])
redis.call('ZREM', KEYS[3], ARGV[2])
redis.call('HDEL', KEYS[4], ARGV[2])
if acknowledged == 0 then return -2 end
redis.call('XDEL', KEYS[1], ARGV[2])
return 1
`)

var redisAIJobDeliveryNackScript = redis.NewScript(`
local token = redis.call('HGET', KEYS[3], ARGV[2])
if not token then return -2 end
if token ~= ARGV[3] then return 0 end
local leaseUntil = tonumber(redis.call('ZSCORE', KEYS[4], ARGV[2]) or '0')
if leaseUntil <= tonumber(ARGV[4]) then return -1 end
local acknowledged = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
if acknowledged == 0 then return -2 end
redis.call('XDEL', KEYS[1], ARGV[2])
redis.call('HDEL', KEYS[3], ARGV[2])
redis.call('ZREM', KEYS[4], ARGV[2])
redis.call('HDEL', KEYS[5], ARGV[2])
if tonumber(ARGV[5]) <= tonumber(ARGV[4]) then
  redis.call('XADD', KEYS[1], '*', 'envelope', ARGV[6], 'attempt_base', ARGV[7], 'last_error', ARGV[8])
else
  redis.call('ZADD', KEYS[2], ARGV[5], ARGV[9])
end
return 1
`)

var redisAIJobDeliveryDeadLetterScript = redis.NewScript(`
local token = redis.call('HGET', KEYS[3], ARGV[2])
if not token then return -2 end
if token ~= ARGV[3] then return 0 end
local leaseUntil = tonumber(redis.call('ZSCORE', KEYS[4], ARGV[2]) or '0')
if leaseUntil <= tonumber(ARGV[4]) then return -1 end
local acknowledged = redis.call('XACK', KEYS[1], ARGV[1], ARGV[2])
if acknowledged == 0 then return -2 end
redis.call('XDEL', KEYS[1], ARGV[2])
redis.call('HDEL', KEYS[3], ARGV[2])
redis.call('ZREM', KEYS[4], ARGV[2])
redis.call('HDEL', KEYS[5], ARGV[2])
redis.call('XADD', KEYS[2], '*', 'envelope', ARGV[5], 'attempt', ARGV[6], 'reason', ARGV[7], 'failed_at', ARGV[4])
return 1
`)
