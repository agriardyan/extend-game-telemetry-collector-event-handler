// Copyright (c) 2025 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/events"
	statistic "github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/pb/accelbyte-asyncapi/social/statistic/v1"
	"github.com/agriardyan/extend-game-telemetry-collector-event-handler/pkg/processor"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/emptypb"
)

// StatItemUpdatedService handles incoming StatItemUpdated events from AccelByte's
// statistic service and routes them to the async processor pipeline.
type StatItemUpdatedService struct {
	statistic.UnimplementedStatisticStatItemUpdatedServiceServer
	namespace string
	proc      *processor.Processor[*events.StatItemUpdatedEvent]
	logger    *slog.Logger
}

func NewStatItemUpdatedService(
	namespace string,
	proc *processor.Processor[*events.StatItemUpdatedEvent],
	logger *slog.Logger,
) *StatItemUpdatedService {
	return &StatItemUpdatedService{
		namespace: namespace,
		proc:      proc,
		logger:    logger.With("component", "stat_item_updated_service"),
	}
}

// OnMessage implements StatisticStatItemUpdatedServiceServer.
func (s *StatItemUpdatedService) OnMessage(ctx context.Context, req *statistic.StatItemUpdated) (*emptypb.Empty, error) {
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}

	// Use namespace from the event envelope; fall back to the service namespace.
	namespace := req.Namespace
	if namespace == "" {
		namespace = s.namespace
	}

	event := &events.StatItemUpdatedEvent{
		Namespace:       namespace,
		UserID:          req.UserId,
		ServerTimestamp: time.Now().UnixMilli(),
		Payload:         req,
	}

	if err := s.proc.Submit(event); err != nil {
		s.logger.Error("failed to submit stat_item_updated event",
			"error", err, "namespace", event.Namespace, "user_id", event.UserID)
		return nil, status.Error(codes.Internal, "failed to process event")
	}

	return &emptypb.Empty{}, nil
}
