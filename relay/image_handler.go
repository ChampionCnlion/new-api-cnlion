package relay

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/relay/channel"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	relayconstant "github.com/QuantumNous/new-api/relay/constant"
	"github.com/QuantumNous/new-api/relay/helper"
	"github.com/QuantumNous/new-api/service"
	"github.com/QuantumNous/new-api/setting/model_setting"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func ImageHelper(c *gin.Context, info *relaycommon.RelayInfo) (newAPIError *types.NewAPIError) {
	info.InitChannelMeta(c)

	imageReq, ok := info.Request.(*dto.ImageRequest)
	if !ok {
		return types.NewErrorWithStatusCode(fmt.Errorf("invalid request type, expected dto.ImageRequest, got %T", info.Request), types.ErrorCodeInvalidRequest, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
	}

	request, err := common.DeepCopy(imageReq)
	if err != nil {
		return types.NewError(fmt.Errorf("failed to copy request to ImageRequest: %w", err), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	err = helper.ModelMappedHelper(c, info, request)
	if err != nil {
		return types.NewError(err, types.ErrorCodeChannelModelMappedError, types.ErrOptionWithSkipRetry())
	}

	adaptor := GetAdaptor(info.ApiType)
	if adaptor == nil {
		return types.NewError(fmt.Errorf("invalid api type: %d", info.ApiType), types.ErrorCodeInvalidApiType, types.ErrOptionWithSkipRetry())
	}
	adaptor.Init(info)

	statusCodeMappingStr := c.GetString("status_code_mapping")
	passThroughEnabled := model_setting.GetGlobalSettings().PassThroughRequestEnabled || info.ChannelSetting.PassThroughBodyEnabled
	if !passThroughEnabled && shouldRelayImageViaChatCompletions(info, request) {
		return relayImageViaChatCompletions(c, info, adaptor, request, statusCodeMappingStr)
	}

	var requestBody io.Reader

	if passThroughEnabled {
		storage, err := common.GetBodyStorage(c)
		if err != nil {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeReadRequestBodyFailed, http.StatusBadRequest, types.ErrOptionWithSkipRetry())
		}
		requestBody = common.ReaderOnly(storage)
	} else {
		convertedRequest, err := adaptor.ConvertImageRequest(c, info, *request)
		if err != nil {
			return types.NewError(err, types.ErrorCodeConvertRequestFailed)
		}
		relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

		switch convertedRequest.(type) {
		case *bytes.Buffer:
			requestBody = convertedRequest.(io.Reader)
		default:
			jsonData, err := common.Marshal(convertedRequest)
			if err != nil {
				return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
			}

			// apply param override
			if len(info.ParamOverride) > 0 {
				jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
				if err != nil {
					return newAPIErrorFromParamOverride(err)
				}
			}

			if common.DebugEnabled {
				logger.LogDebug(c, fmt.Sprintf("image request body: %s", string(jsonData)))
			}
			requestBody = bytes.NewBuffer(jsonData)
		}
	}

	resp, err := adaptor.DoRequest(c, info, requestBody)
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}
	var httpResp *http.Response
	if resp != nil {
		httpResp = resp.(*http.Response)
		info.IsStream = info.IsStream || strings.HasPrefix(httpResp.Header.Get("Content-Type"), "text/event-stream")
		if httpResp.StatusCode != http.StatusOK {
			if httpResp.StatusCode == http.StatusCreated && info.ApiType == constant.APITypeReplicate {
				// replicate channel returns 201 Created when using Prefer: wait, treat it as success.
				httpResp.StatusCode = http.StatusOK
			} else {
				newAPIError = service.RelayErrorHandler(c.Request.Context(), httpResp, false)
				// reset status code 重置状态码
				service.ResetStatusCode(newAPIError, statusCodeMappingStr)
				return newAPIError
			}
		}
	}

	usage, newAPIError := adaptor.DoResponse(c, httpResp, info)
	if newAPIError != nil {
		// reset status code 重置状态码
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	postImageConsumeQuota(c, info, request, usage.(*dto.Usage))
	return nil
}

func shouldRelayImageViaChatCompletions(info *relaycommon.RelayInfo, request *dto.ImageRequest) bool {
	if info == nil || request == nil {
		return false
	}
	if info.RelayMode != relayconstant.RelayModeImagesGenerations {
		return false
	}
	if info.ApiType != constant.APITypeOpenAI {
		return false
	}

	for _, modelName := range []string{request.Model, info.OriginModelName, info.UpstreamModelName} {
		switch strings.TrimSpace(modelName) {
		case "gpt-image-2", "codex-gpt-image-2":
			return true
		}
	}
	return false
}

func relayImageViaChatCompletions(
	c *gin.Context,
	info *relaycommon.RelayInfo,
	adaptor channel.Adaptor,
	request *dto.ImageRequest,
	statusCodeMappingStr string,
) *types.NewAPIError {
	chatRequest := buildImageChatCompletionRequest(request)

	savedRelayMode := info.RelayMode
	savedRequestURLPath := info.RequestURLPath
	savedRequest := info.Request
	savedIsStream := info.IsStream
	defer func() {
		info.RelayMode = savedRelayMode
		info.RequestURLPath = savedRequestURLPath
		info.Request = savedRequest
		info.IsStream = savedIsStream
	}()

	info.RelayMode = relayconstant.RelayModeChatCompletions
	info.RequestURLPath = "/v1/chat/completions"
	info.Request = chatRequest
	info.IsStream = false

	convertedRequest, err := adaptor.ConvertOpenAIRequest(c, info, chatRequest)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}
	relaycommon.AppendRequestConversionFromRequest(info, convertedRequest)

	jsonData, err := common.Marshal(convertedRequest)
	if err != nil {
		return types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
	}

	jsonData, err = relaycommon.RemoveDisabledFields(jsonData, info.ChannelOtherSettings, info.ChannelSetting.PassThroughBodyEnabled)
	if err != nil {
		return types.NewError(err, types.ErrorCodeConvertRequestFailed, types.ErrOptionWithSkipRetry())
	}

	if len(info.ParamOverride) > 0 {
		jsonData, err = relaycommon.ApplyParamOverrideWithRelayInfo(jsonData, info)
		if err != nil {
			return newAPIErrorFromParamOverride(err)
		}
	}

	logger.LogDebug(c, fmt.Sprintf("image via chat request body: %s", string(jsonData)))

	resp, err := adaptor.DoRequest(c, info, bytes.NewBuffer(jsonData))
	if err != nil {
		return types.NewOpenAIError(err, types.ErrorCodeDoRequestFailed, http.StatusInternalServerError)
	}

	httpResp, _ := resp.(*http.Response)
	if httpResp == nil {
		return types.NewOpenAIError(fmt.Errorf("invalid response type: %T", resp), types.ErrorCodeBadResponse, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}
	if httpResp.StatusCode != http.StatusOK {
		newAPIError := service.RelayErrorHandler(c.Request.Context(), httpResp, false)
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	usage, newAPIError := writeImageResponseFromChatCompletion(c, httpResp, request)
	if newAPIError != nil {
		service.ResetStatusCode(newAPIError, statusCodeMappingStr)
		return newAPIError
	}

	postImageConsumeQuota(c, info, request, usage)
	return nil
}

func buildImageChatCompletionRequest(request *dto.ImageRequest) *dto.GeneralOpenAIRequest {
	chatRequest := &dto.GeneralOpenAIRequest{
		Model: request.Model,
		Messages: []dto.Message{
			{
				Role:    "user",
				Content: request.Prompt,
			},
		},
	}
	if request.N != nil {
		n := int(*request.N)
		chatRequest.N = &n
	}
	return chatRequest
}

func writeImageResponseFromChatCompletion(c *gin.Context, resp *http.Response, request *dto.ImageRequest) (*dto.Usage, *types.NewAPIError) {
	defer service.CloseResponseBodyGracefully(resp)

	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeReadResponseBodyFailed, http.StatusInternalServerError)
	}

	var chatResponse dto.OpenAITextResponse
	if err := common.Unmarshal(responseBody, &chatResponse); err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError)
	}

	if oaiError := chatResponse.GetOpenAIError(); oaiError != nil && oaiError.Type != "" {
		return nil, types.WithOpenAIError(*oaiError, resp.StatusCode)
	}

	imageResponse, err := convertChatCompletionToImageResponse(&chatResponse, request)
	if err != nil {
		return nil, types.NewOpenAIError(err, types.ErrorCodeBadResponseBody, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
	}

	imageResponseBody, err := common.Marshal(imageResponse)
	if err != nil {
		return nil, types.NewError(err, types.ErrorCodeJsonMarshalFailed, types.ErrOptionWithSkipRetry())
	}

	service.IOCopyBytesGracefully(c, resp, imageResponseBody)

	usage := chatResponse.Usage
	if usage.TotalTokens == 0 {
		usage.TotalTokens = 1
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = 1
	}
	if usage.TotalTokens < usage.PromptTokens {
		usage.TotalTokens = usage.PromptTokens
	}
	return &usage, nil
}

func convertChatCompletionToImageResponse(chatResponse *dto.OpenAITextResponse, request *dto.ImageRequest) (*dto.ImageResponse, error) {
	if chatResponse == nil {
		return nil, fmt.Errorf("chat completion response is nil")
	}

	imageResponse := &dto.ImageResponse{
		Created: imageResponseCreatedAt(chatResponse.Created),
		Data:    make([]dto.ImageData, 0),
	}

	wantsURL := strings.EqualFold(strings.TrimSpace(request.ResponseFormat), "url")
	for _, choice := range chatResponse.Choices {
		for _, dataURL := range extractMarkdownImageDataURLs(choice.Message.StringContent()) {
			imageData := dto.ImageData{
				RevisedPrompt: request.Prompt,
			}
			if wantsURL {
				imageData.Url = dataURL
			} else {
				commaIndex := strings.Index(dataURL, ",")
				if commaIndex == -1 || commaIndex == len(dataURL)-1 {
					continue
				}
				imageData.B64Json = dataURL[commaIndex+1:]
			}
			imageResponse.Data = append(imageResponse.Data, imageData)
		}
	}

	if len(imageResponse.Data) == 0 {
		return nil, fmt.Errorf("no image found in chat completion response")
	}
	return imageResponse, nil
}

func imageResponseCreatedAt(raw any) int64 {
	switch value := raw.(type) {
	case int64:
		return value
	case int:
		return int64(value)
	case float64:
		return int64(value)
	case string:
		if ts, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64); err == nil {
			return ts
		}
	}
	return time.Now().Unix()
}

func extractMarkdownImageDataURLs(text string) []string {
	var dataURLs []string
	searchFrom := 0
	for searchFrom < len(text) {
		imageStart := strings.Index(text[searchFrom:], "![")
		if imageStart == -1 {
			break
		}
		imageStart += searchFrom

		urlMarker := strings.Index(text[imageStart:], "](data:image/")
		if urlMarker == -1 {
			searchFrom = imageStart + 2
			continue
		}
		urlStart := imageStart + urlMarker + 2
		urlEnd := strings.Index(text[urlStart:], ")")
		if urlEnd == -1 {
			break
		}
		urlEnd += urlStart

		dataURL := text[urlStart:urlEnd]
		if strings.Contains(dataURL, ";base64,") {
			dataURLs = append(dataURLs, dataURL)
		}
		searchFrom = urlEnd + 1
	}
	return dataURLs
}

func postImageConsumeQuota(c *gin.Context, info *relaycommon.RelayInfo, request *dto.ImageRequest, usage *dto.Usage) {
	imageN := uint(1)
	if request.N != nil {
		imageN = *request.N
	}

	if _, hasN := info.PriceData.OtherRatios["n"]; !hasN {
		info.PriceData.AddOtherRatio("n", float64(imageN))
	}

	if usage.TotalTokens == 0 {
		usage.TotalTokens = 1
	}
	if usage.PromptTokens == 0 {
		usage.PromptTokens = 1
	}

	quality := "standard"
	if request.Quality == "hd" {
		quality = "hd"
	}

	var logContent []string
	if len(request.Size) > 0 {
		logContent = append(logContent, fmt.Sprintf("大小 %s", request.Size))
	}
	if len(quality) > 0 {
		logContent = append(logContent, fmt.Sprintf("品质 %s", quality))
	}
	if imageN > 0 {
		logContent = append(logContent, fmt.Sprintf("生成数量 %d", imageN))
	}

	service.PostTextConsumeQuota(c, info, usage, logContent)
}
