// Copyright (c) 2023 AccelByte Inc. All Rights Reserved.
// This is licensed software from AccelByte Inc, for limitations
// and restrictions contact your company contract manager.

package common

import (
	"context"

	log "github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	oteltrace "go.opentelemetry.io/otel/trace"
)

const (
	traceIdLogField = "traceID"
	tracerName      = "event-handler"
)

// GetScopeFromContext used to get Scope from the gRPC context
func GetScopeFromContext(ctx context.Context, name string) *Scope {
	return ChildScopeFromRemoteScope(ctx, name)
}

func ChildScopeFromRemoteScope(ctx context.Context, name string) *Scope {
	tracer := otel.Tracer(tracerName)
	tracerCtx, span := tracer.Start(ctx, name)
	traceID := span.SpanContext().TraceID().String()

	return &Scope{
		Ctx:     tracerCtx,
		TraceID: traceID,
		span:    span,
		Log:     log.WithField(traceIdLogField, traceID),
	}
}

// Scope used as the envelope to combine and transport request-related information by the chain of function calls
type Scope struct {
	Ctx     context.Context
	TraceID string
	span    oteltrace.Span
	Log     *log.Entry
}

// SetLogger allows for setting a different logger than the default std logger. This is mostly useful for testing.
func (s *Scope) SetLogger(logger *log.Entry) {
	s.Log = log.WithField(traceIdLogField, s.TraceID)
}

// Finish finishes current scope
func (s *Scope) Finish() {
	s.span.End()
}

// TraceEvent creates a human-readable message on the span -- typically representing that "something happened"
func (s *Scope) TraceEvent(eventMessage string) {
	s.span.AddEvent(eventMessage)
}

// TraceError records an error and sets the span status with that error so it can be viewed
func (s *Scope) TraceError(err error) {
	s.span.RecordError(err)
	s.span.SetStatus(codes.Error, err.Error())
}

// TraceTag sends a tag into tracer
func (s *Scope) TraceTag(key, value string) {
	s.AddBaggage(key, value)
}

// AddBaggage adds metadata to the span. Use SetAttributes for other value objects besides a String
func (s *Scope) AddBaggage(key string, value string) {
	s.span.SetAttributes(attribute.String(key, value))
}

// SetAttributes adds attributes onto a span based on the value object type
func (s *Scope) SetAttributes(key string, value interface{}) {
	switch v := value.(type) {
	case bool:
		s.span.SetAttributes(attribute.Bool(key, v))
	case string:
		s.span.SetAttributes(attribute.String(key, v))
	case int:
		s.span.SetAttributes(attribute.Int(key, v))
	case int64:
		s.span.SetAttributes(attribute.Int64(key, v))
	case float64:
		s.span.SetAttributes(attribute.Float64(key, v))
	case []bool:
		s.span.SetAttributes(attribute.BoolSlice(key, v))
	case []string:
		s.span.SetAttributes(attribute.StringSlice(key, v))
	case []int:
		s.span.SetAttributes(attribute.IntSlice(key, v))
	case []int64:
		s.span.SetAttributes(attribute.Int64Slice(key, v))
	case []float64:
		s.span.SetAttributes(attribute.Float64Slice(key, v))
	default:
		s.Log.Errorf("could not set a span attribute of type %T", value)
	}
}

// GetSpanContextString gets scope span context string
func (s *Scope) GetSpanContextString() string {
	return s.span.SpanContext().SpanID().String()
}

// NewChildScope creates new child Scope
func (s *Scope) NewChildScope(name string) *Scope {
	tracer := s.span.TracerProvider().Tracer(tracerName)
	ctx, span := tracer.Start(s.Ctx, name)

	return &Scope{
		Ctx:     ctx,
		TraceID: s.TraceID,
		span:    span,
		Log:     s.Log,
	}
}
