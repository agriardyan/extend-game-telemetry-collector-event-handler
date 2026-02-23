// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	oauthpb "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb/accelbyte-asyncapi/iam/oauth/v1"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/processor"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// OauthTokenGeneratedService handles incoming OauthTokenGenerated events from AccelByte's
// IAM service and routes them to the async processor pipeline.
type OauthTokenGeneratedService struct {
	oauthpb.UnimplementedOauthTokenOauthTokenGeneratedServiceServer
	namespace string
	proc      *processor.Processor[*events.OauthTokenGeneratedEvent]
	logger    *slog.Logger
}

func NewOauthTokenGeneratedService(
	namespace string,
	proc *processor.Processor[*events.OauthTokenGeneratedEvent],
	logger *slog.Logger,
) *OauthTokenGeneratedService {
	return &OauthTokenGeneratedService{
		namespace: namespace,
		proc:      proc,
		logger:    logger.With("component", "oauth_token_generated_service"),
	}
}

// OnMessage implements OauthTokenOauthTokenGeneratedServiceServer.
func (s *OauthTokenGeneratedService) OnMessage(ctx context.Context, req *oauthpb.OauthTokenGenerated) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	namespace := req.Namespace
	if namespace == "" {
		namespace = s.namespace
	}

	var payload *events.OauthPayload
	if req.Payload != nil && req.Payload.Oauth != nil {
		payload = &events.OauthPayload{
			ClientID:     req.Payload.Oauth.ClientId,
			ResponseType: req.Payload.Oauth.ResponseType,
			PlatformID:   req.Payload.Oauth.PlatformId,
		}
	}

	event := &events.OauthTokenGeneratedEvent{
		ID:              req.Id,
		Version:         req.Version,
		Name:            req.Name,
		Namespace:       namespace,
		UserID:          req.UserId,
		SessionID:       req.SessionId,
		TraceID:         req.TraceId,
		Timestamp:       req.Timestamp,
		ServerTimestamp: time.Now().UnixMilli(),
		Payload:         payload,
	}

	if err := s.proc.Submit(event); err != nil {
		s.logger.Error("failed to submit oauth_token_generated event",
			"error", err, "namespace", event.Namespace, "user_id", event.UserID)
		return nil, status.Error(codes.Internal, "failed to process event")
	}

	return &emptypb.Empty{}, nil
}
