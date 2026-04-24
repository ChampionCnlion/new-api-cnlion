package relay

import (
	"testing"

	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/stretchr/testify/require"
)

func TestShouldRelayImageViaChatCompletions(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeImagesGenerations,
		OriginModelName: "gpt-image-2",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType: constant.APITypeOpenAI,
		},
	}
	request := &dto.ImageRequest{Model: "gpt-image-2"}

	require.True(t, shouldRelayImageViaChatCompletions(info, request))
}

func TestShouldRelayImageViaChatCompletionsRejectsUnsupportedModel(t *testing.T) {
	t.Parallel()

	info := &relaycommon.RelayInfo{
		RelayMode:       relayconstant.RelayModeImagesGenerations,
		OriginModelName: "gpt-image-1",
		ChannelMeta: &relaycommon.ChannelMeta{
			ApiType: constant.APITypeOpenAI,
		},
	}
	request := &dto.ImageRequest{Model: "gpt-image-1"}

	require.False(t, shouldRelayImageViaChatCompletions(info, request))
}

func TestConvertChatCompletionToImageResponseReturnsBase64ByDefault(t *testing.T) {
	t.Parallel()

	chatResponse := &dto.OpenAITextResponse{
		Created: float64(1710000000),
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message: dto.Message{
					Role:    "assistant",
					Content: "![image_1](data:image/png;base64,Zmlyc3Q=)\n\n![image_2](data:image/jpeg;base64,c2Vjb25k)",
				},
			},
		},
	}
	request := &dto.ImageRequest{
		Model:  "gpt-image-2",
		Prompt: "draw a cat",
	}

	imageResponse, err := convertChatCompletionToImageResponse(chatResponse, request)
	require.NoError(t, err)
	require.Equal(t, int64(1710000000), imageResponse.Created)
	require.Len(t, imageResponse.Data, 2)
	require.Equal(t, "Zmlyc3Q=", imageResponse.Data[0].B64Json)
	require.Equal(t, "c2Vjb25k", imageResponse.Data[1].B64Json)
	require.Empty(t, imageResponse.Data[0].Url)
}

func TestConvertChatCompletionToImageResponseReturnsDataURLWhenRequested(t *testing.T) {
	t.Parallel()

	chatResponse := &dto.OpenAITextResponse{
		Choices: []dto.OpenAITextResponseChoice{
			{
				Message: dto.Message{
					Role:    "assistant",
					Content: "![image_1](data:image/png;base64,Zmlyc3Q=)",
				},
			},
		},
	}
	request := &dto.ImageRequest{
		Model:          "gpt-image-2",
		Prompt:         "draw a cat",
		ResponseFormat: "url",
	}

	imageResponse, err := convertChatCompletionToImageResponse(chatResponse, request)
	require.NoError(t, err)
	require.Len(t, imageResponse.Data, 1)
	require.Equal(t, "data:image/png;base64,Zmlyc3Q=", imageResponse.Data[0].Url)
	require.Empty(t, imageResponse.Data[0].B64Json)
}
