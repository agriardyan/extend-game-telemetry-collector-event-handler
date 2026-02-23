// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"strings"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	pb "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/processor"

	"github.com/AccelByte/accelbyte-go-sdk/services-api/pkg/repository"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// TelemetryService implements the gRPC Service server for all telemetry endpoints.
// Namespace is static and injected at construction time from the environment.
// UserID is always derived from the Bearer JWT — never trusted from the request body.
type TelemetryService struct {
	pb.UnimplementedServiceServer
	namespace        string
	tokenRepo        repository.TokenRepository
	configRepo       repository.ConfigRepository
	refreshRepo      repository.RefreshTokenRepository
	userBehaviorProc *processor.Processor[*events.UserBehaviorEvent]
	gameplayProc     *processor.Processor[*events.GameplayEvent]
	performanceProc  *processor.Processor[*events.PerformanceEvent]
	logger           *slog.Logger
}

func NewTelemetryService(
	namespace string,
	tokenRepo repository.TokenRepository,
	configRepo repository.ConfigRepository,
	refreshRepo repository.RefreshTokenRepository,
	userBehaviorProc *processor.Processor[*events.UserBehaviorEvent],
	gameplayProc *processor.Processor[*events.GameplayEvent],
	performanceProc *processor.Processor[*events.PerformanceEvent],
	logger *slog.Logger,
) *TelemetryService {
	return &TelemetryService{
		namespace:        namespace,
		tokenRepo:        tokenRepo,
		configRepo:       configRepo,
		refreshRepo:      refreshRepo,
		userBehaviorProc: userBehaviorProc,
		gameplayProc:     gameplayProc,
		performanceProc:  performanceProc,
		logger:           logger.With("component", "telemetry_service"),
	}
}

// CreateUserBehaviorTelemetry handles POST /v1/telemetry/user-behavior
func (s *TelemetryService) CreateUserBehaviorTelemetry(
	ctx context.Context, req *pb.CreateUserBehaviorTelemetryRequest,
) (*pb.CreateTelemetryResponse, error) {
	if req.EventId == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}
	if req.User == nil {
		return nil, status.Error(codes.InvalidArgument, "user is required")
	}

	userID, err := s.extractUserID(ctx)
	if err != nil {
		return nil, err
	}

	event := &events.UserBehaviorEvent{
		Namespace:       s.namespace,
		UserID:          userID,
		ServerTimestamp: time.Now().UnixMilli(),
		SourceIP:        s.extractSourceIP(ctx),
		Payload:         req,
	}

	if err := s.userBehaviorProc.Submit(event); err != nil {
		s.logger.Error("failed to submit user behavior event",
			"error", err, "namespace", event.Namespace, "user_id", event.UserID)
		return nil, status.Error(codes.Internal, "failed to process telemetry event")
	}
	return &pb.CreateTelemetryResponse{}, nil
}

// CreateGameplayTelemetry handles POST /v1/telemetry/gameplay
func (s *TelemetryService) CreateGameplayTelemetry(
	ctx context.Context, req *pb.CreateGameplayTelemetryRequest,
) (*pb.CreateTelemetryResponse, error) {
	if req.EventId == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}
	if req.Data == nil {
		return nil, status.Error(codes.InvalidArgument, "data is required")
	}

	userID, err := s.extractUserID(ctx)
	if err != nil {
		return nil, err
	}

	event := &events.GameplayEvent{
		Namespace:       s.namespace,
		UserID:          userID,
		ServerTimestamp: time.Now().UnixMilli(),
		SourceIP:        s.extractSourceIP(ctx),
		Payload:         req,
	}

	if err := s.gameplayProc.Submit(event); err != nil {
		s.logger.Error("failed to submit gameplay event",
			"error", err, "namespace", event.Namespace, "user_id", event.UserID)
		return nil, status.Error(codes.Internal, "failed to process telemetry event")
	}
	return &pb.CreateTelemetryResponse{}, nil
}

// CreatePerformanceTelemetry handles POST /v1/telemetry/performance
func (s *TelemetryService) CreatePerformanceTelemetry(
	ctx context.Context, req *pb.CreatePerformanceTelemetryRequest,
) (*pb.CreateTelemetryResponse, error) {
	if req.EventId == "" {
		return nil, status.Error(codes.InvalidArgument, "event_id is required")
	}
	if req.Metrics == nil {
		return nil, status.Error(codes.InvalidArgument, "metrics is required")
	}

	userID, err := s.extractUserID(ctx)
	if err != nil {
		return nil, err
	}

	event := &events.PerformanceEvent{
		Namespace:       s.namespace,
		UserID:          userID,
		ServerTimestamp: time.Now().UnixMilli(),
		SourceIP:        s.extractSourceIP(ctx),
		Payload:         req,
	}

	if err := s.performanceProc.Submit(event); err != nil {
		s.logger.Error("failed to submit performance event",
			"error", err, "namespace", event.Namespace, "user_id", event.UserID)
		return nil, status.Error(codes.Internal, "failed to process telemetry event")
	}
	return &pb.CreateTelemetryResponse{}, nil
}

// extractUserID decodes the sub claim from the JWT in the Authorization header.
// It does not re-validate the signature here — the auth interceptor already did that.
func (s *TelemetryService) extractUserID(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", status.Error(codes.Unauthenticated, "missing metadata")
	}

	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", status.Error(codes.Unauthenticated, "missing authorization header")
	}

	token := strings.TrimPrefix(vals[0], "Bearer ")
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return "", status.Error(codes.Unauthenticated, "malformed JWT")
	}

	// Decode the payload (second part). Add padding if needed.
	payload := parts[1]
	if rem := len(payload) % 4; rem != 0 {
		payload += strings.Repeat("=", 4-rem)
	}

	raw, err := base64.URLEncoding.DecodeString(payload)
	if err != nil {
		return "", status.Error(codes.Unauthenticated, "malformed JWT payload")
	}

	var claims struct {
		Sub    string `json:"sub"`
		UserID string `json:"user_id"` // AccelByte sometimes uses user_id instead
	}
	if err := json.Unmarshal(raw, &claims); err != nil {
		return "", status.Error(codes.Unauthenticated, "malformed JWT claims")
	}

	userID := claims.Sub
	if userID == "" {
		userID = claims.UserID
	}
	if userID == "" {
		return "", status.Error(codes.Unauthenticated, "user ID not found in token")
	}

	return userID, nil
}

// extractSourceIP gets the client IP from X-Forwarded-For or the peer address.
func (s *TelemetryService) extractSourceIP(ctx context.Context) string {
	if md, ok := metadata.FromIncomingContext(ctx); ok {
		if xff := md.Get("x-forwarded-for"); len(xff) > 0 {
			return xff[0]
		}
	}
	if p, ok := peer.FromContext(ctx); ok {
		return p.Addr.String()
	}
	return "unknown"
}
