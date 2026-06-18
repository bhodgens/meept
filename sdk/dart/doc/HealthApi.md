# meept_client.api.HealthApi

## Load the API package
```dart
import 'package:meept_client/api.dart';
```

All URIs are relative to *http://localhost:8081*

Method | HTTP request | Description
------------- | ------------- | -------------
[**healthGet**](HealthApi.md#healthget) | **GET** /health | s.handleHealth


# **healthGet**
> healthGet()

s.handleHealth

### Example
```dart
import 'package:meept_client/api.dart';
// TODO Configure API key authorization: ApiKeyAuth
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKey = 'YOUR_API_KEY';
// uncomment below to setup prefix (e.g. Bearer) for API key, if needed
//defaultApiClient.getAuthentication<ApiKeyAuth>('ApiKeyAuth').apiKeyPrefix = 'Bearer';

final api = MeeptClient().getHealthApi();

try {
    api.healthGet();
} on DioException catch (e) {
    print('Exception when calling HealthApi->healthGet: $e\n');
}
```

### Parameters
This endpoint does not need any parameter.

### Return type

void (empty response body)

### Authorization

[ApiKeyAuth](../README.md#ApiKeyAuth)

### HTTP request headers

 - **Content-Type**: Not defined
 - **Accept**: Not defined

[[Back to top]](#) [[Back to API list]](../README.md#documentation-for-api-endpoints) [[Back to Model list]](../README.md#documentation-for-models) [[Back to README]](../README.md)

