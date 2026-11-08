package importer

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/v2"
)

// BatchRequest is one item of a batch.
type BatchRequest struct {
	ID   string // custom_id: letters, digits, '_' and '-', max 64 chars
	User string
}

// BatchOutput is the result for one request.
type BatchOutput struct {
	Text string
	Err  string
}

// SubmitBatch sends all requests (sharing one cached system prompt) and returns the batch id.
func (c *Claude) SubmitBatch(ctx context.Context, system string, reqs []BatchRequest) (string, error) {
	var items []anthropic.MessageBatchNewParamsRequest
	for _, r := range reqs {
		items = append(items, anthropic.MessageBatchNewParamsRequest{
			CustomID: r.ID,
			Params: anthropic.MessageBatchNewParamsRequestParams{
				Model:     anthropic.Model(c.model),
				MaxTokens: 16000,
				System:    []anthropic.TextBlockParam{{Text: system, CacheControl: anthropic.NewCacheControlEphemeralParam()}},
				Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock(r.User))},
			},
		})
	}
	b, err := c.client.Messages.Batches.New(ctx, anthropic.MessageBatchNewParams{Requests: items})
	if err != nil {
		return "", err
	}
	return b.ID, nil
}

// WaitBatch polls until the batch has ended.
func (c *Claude) WaitBatch(ctx context.Context, id string, every time.Duration, progress func(processing, succeeded, errored int64)) error {
	for {
		b, err := c.client.Messages.Batches.Get(ctx, id, anthropic.MessageBatchGetParams{})
		if err != nil {
			return err
		}
		if progress != nil {
			progress(b.RequestCounts.Processing, b.RequestCounts.Succeeded, b.RequestCounts.Errored)
		}
		if b.ProcessingStatus == anthropic.MessageBatchProcessingStatusEnded {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(every):
		}
	}
}

// BatchResults collects all results keyed by custom id.
func (c *Claude) BatchResults(ctx context.Context, id string) (map[string]BatchOutput, error) {
	out := map[string]BatchOutput{}
	stream := c.client.Messages.Batches.ResultsStreaming(ctx, id, anthropic.MessageBatchResultsParams{})
	for stream.Next() {
		item := stream.Current()
		switch item.Result.Type {
		case "succeeded":
			if item.Result.Message.StopReason == anthropic.StopReasonRefusal {
				out[item.CustomID] = BatchOutput{Err: "refused"}
				continue
			}
			var sb strings.Builder
			for _, block := range item.Result.Message.Content {
				if t, ok := block.AsAny().(anthropic.TextBlock); ok {
					sb.WriteString(t.Text)
				}
			}
			out[item.CustomID] = BatchOutput{Text: sb.String()}
		case "errored":
			out[item.CustomID] = BatchOutput{Err: item.Result.Error.Error.Message}
		default:
			out[item.CustomID] = BatchOutput{Err: item.Result.Type}
		}
	}
	if err := stream.Err(); err != nil {
		return out, err
	}
	if len(out) == 0 {
		return out, errors.New("batch returned no results")
	}
	return out, nil
}

var _ = fmt.Sprint
