# meept_client.api.WebSocketApi

## Load the API package
```dart
import 'package:meept_client/api.dart';
```

All URIs are relative to *http://localhost:8081*

Method | HTTP request | Description
------------- | ------------- | -------------
[**getWebSocket**](WebSocketApi.md#getwebsocket) | **GET** /ws | WebSocket connection for real-time updates


# **getWebSocket**
> getWebSocket(sessionId)

WebSocket connection for real-time updates

Establishes a WebSocket connection for receiving real-time agent progress events. Clients can subscribe to specific sessions via subscribe messages. 

### Example
```dart
import 'package:meept_client/api.dart';

final api_instance = WebSocketApi();
final sessionId = sessionId_example; // String | Optional session ID to filter events

try {
    api_instance.getWebSocket(sessionId);
} catch (e) {
    print('Exception when calling WebSocketApi->getWebSocket: $e\n');
}
```

### Parameters

Name | Type | Description  | Notes
------------- | ------------- | ------------- | -------------
 **sessionId** | **String**| Optional session ID to filter events | [optional] 

### Return type

void (empty response body)

### Authorization

No authorization required

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

