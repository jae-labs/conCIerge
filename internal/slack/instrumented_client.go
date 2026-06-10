package slack

import (
	"context"
	"errors"
	"time"

	"github.com/slack-go/slack"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

func (h *Handler) postMessageCtx(ctx context.Context, channelID, operation string, opts ...slack.MsgOption) string {
	attrs := []attribute.KeyValue{
		attribute.String("slack.api_method", "post_message"),
		attribute.String("slack.channel", channelID),
		attribute.String("slack.operation", operation),
	}
	ctx, span, started := h.startSlackAPICall(ctx, "post_message", attrs...)
	_, ts, err := h.api.PostMessage(channelID, opts...)
	h.finishSlackAPICall(ctx, span, started, err, attrs...)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to post Slack message", "operation", operation, "channel", channelID, "error", err)
		return ""
	}
	return ts
}

func (h *Handler) postEphemeralCtx(ctx context.Context, channelID, userID, operation string, opts ...slack.MsgOption) {
	attrs := []attribute.KeyValue{
		attribute.String("slack.api_method", "post_ephemeral"),
		attribute.String("slack.channel", channelID),
		attribute.String("slack.user_id", userID),
		attribute.String("slack.operation", operation),
	}
	ctx, span, started := h.startSlackAPICall(ctx, "post_ephemeral", attrs...)
	_, err := h.api.PostEphemeral(channelID, userID, opts...)
	h.finishSlackAPICall(ctx, span, started, err, attrs...)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to post ephemeral Slack message", "operation", operation, "channel", channelID, "user", userID, "error", err)
	}
}

func (h *Handler) updateChannelMessageCtx(ctx context.Context, channelID, messageTS, operation string, opts ...slack.MsgOption) {
	attrs := []attribute.KeyValue{
		attribute.String("slack.api_method", "update_message"),
		attribute.String("slack.channel", channelID),
		attribute.String("slack.message_ts", messageTS),
		attribute.String("slack.operation", operation),
	}
	ctx, span, started := h.startSlackAPICall(ctx, "update_message", attrs...)
	_, _, _, err := h.api.UpdateMessage(channelID, messageTS, opts...)
	h.finishSlackAPICall(ctx, span, started, err, attrs...)
	if err != nil {
		h.logger.ErrorContext(ctx, "failed to update Slack message", "operation", operation, "channel", channelID, "message_ts", messageTS, "error", err)
	}
}

func (h *Handler) startSlackAPICall(ctx context.Context, name string, attrs ...attribute.KeyValue) (context.Context, trace.Span, time.Time) {
	ctx, span := h.tracer.Start(ctx, "slack.api."+name,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attrs...),
	)
	return ctx, span, time.Now()
}

func (h *Handler) finishSlackAPICall(ctx context.Context, span trace.Span, started time.Time, err error, attrs ...attribute.KeyValue) {
	outcome := "ok"
	if err != nil {
		outcome = "error"
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	} else {
		span.SetStatus(codes.Ok, "")
	}
	attrs = append(attrs, attribute.String("outcome", outcome))
	h.slackAPITotal.Add(ctx, 1, metric.WithAttributes(attrs...))
	h.slackAPIDuration.Record(ctx, time.Since(started).Seconds(), metric.WithAttributes(attrs...))
	span.End()
}

func (h *Handler) openView(ctx context.Context, triggerID string, modal slack.ModalViewRequest) error {
	attrs := []attribute.KeyValue{
		attribute.String("slack.api_method", "open_view"),
		attribute.String("slack.trigger_id", triggerID),
		attribute.String("slack.callback_id", modal.CallbackID),
	}
	if err := validateModalViewRequest(modal); err != nil {
		h.logger.ErrorContext(ctx, "refusing invalid open view modal",
			"callback_id", modal.CallbackID,
			"title", modal.Title.Text,
			"blocks_count", len(modal.Blocks.BlockSet),
			"private_metadata_len", len(modal.PrivateMetadata),
			"error", err,
		)
		return err
	}
	ctx, span, started := h.startSlackAPICall(ctx, "open_view", attrs...)
	_, err := h.api.OpenView(triggerID, modal)
	h.finishSlackAPICall(ctx, span, started, err, attrs...)
	if err != nil {
		var slackErr slack.SlackErrorResponse
		if errors.As(err, &slackErr) {
			h.logger.ErrorContext(ctx, "slack rejected open view",
				"callback_id", modal.CallbackID,
				"title", modal.Title.Text,
				"blocks_count", len(modal.Blocks.BlockSet),
				"private_metadata_len", len(modal.PrivateMetadata),
				"slack_error", slackErr.Err,
				"slack_messages", slackErr.ResponseMetadata.Messages,
				"slack_errors", slackErr.Errors,
			)
		} else {
			h.logger.ErrorContext(ctx, "failed to open view",
				"callback_id", modal.CallbackID,
				"title", modal.Title.Text,
				"blocks_count", len(modal.Blocks.BlockSet),
				"private_metadata_len", len(modal.PrivateMetadata),
				"error", err,
			)
		}
	}
	return err
}

func (h *Handler) publishView(ctx context.Context, userID string, view slack.HomeTabViewRequest) error {
	attrs := []attribute.KeyValue{
		attribute.String("slack.api_method", "publish_view"),
		attribute.String("slack.user_id", userID),
	}
	ctx, span, started := h.startSlackAPICall(ctx, "publish_view", attrs...)
	_, err := h.api.PublishView(userID, view, "")
	h.finishSlackAPICall(ctx, span, started, err, attrs...)
	return err
}

func (h *Handler) getUserInfo(ctx context.Context, userID string) (*slack.User, error) {
	attrs := []attribute.KeyValue{
		attribute.String("slack.api_method", "get_user_info"),
		attribute.String("slack.user_id", userID),
	}
	ctx, span, started := h.startSlackAPICall(ctx, "get_user_info", attrs...)
	user, err := h.api.GetUserInfo(userID)
	h.finishSlackAPICall(ctx, span, started, err, attrs...)
	return user, err
}

func (h *Handler) getPermalink(ctx context.Context, params *slack.PermalinkParameters) (string, error) {
	attrs := []attribute.KeyValue{
		attribute.String("slack.api_method", "get_permalink"),
		attribute.String("slack.channel", params.Channel),
		attribute.String("slack.message_ts", params.Ts),
	}
	ctx, span, started := h.startSlackAPICall(ctx, "get_permalink", attrs...)
	link, err := h.api.GetPermalink(params)
	h.finishSlackAPICall(ctx, span, started, err, attrs...)
	return link, err
}
