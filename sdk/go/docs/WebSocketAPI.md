# \WebSocketAPI

All URIs are relative to *http://localhost:8081*

Method | HTTP request | Description
------------- | ------------- | -------------
[**GetWebSocket**](WebSocketAPI.md#GetWebSocket) | **Get** /ws | WebSocket connection for real-time updates



## GetWebSocket

> GetWebSocket(ctx).SessionId(sessionId).Execute()

WebSocket connection for real-time updates



### Example

```go
package main

import (
	"context"
	"fmt"
	"os"
	openapiclient "github.com/GIT_USER_ID/GIT_REPO_ID"
)

func main() {
	sessionId := "sessionId_example" // string | Optional session ID to filter events (optional)

	configuration := openapiclient.NewConfiguration()
	apiClient := openapiclient.NewAPIClient(configuration)
	r, err := apiClient.WebSocketAPI.GetWebSocket(context.Background()).SessionId(sessionId).Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error when calling `WebSocketAPI.GetWebSocket``: %v\n", err)
		fmt.Fprintf(os.Stderr, "Full HTTP response: %v\n", r)
	}
}
```

### Path Parameters



### Other Parameters

Other parameters are passed through a pointer to a apiGetWebSocketRequest struct via the builder pattern


Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **sessionId** | **string** | Optional session ID to filter events | 

### Return type

 (empty response body)

### Authorization

No authorization required

### HTTP request headers

- **Content-Type**: Not defined
- **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints)
[[Back to Model list]](../README.md#documentation-for-models)
[[Back to README]](../README.md)

