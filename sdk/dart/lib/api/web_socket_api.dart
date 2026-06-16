//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//
// @dart=2.18

// ignore_for_file: unused_element, unused_import
// ignore_for_file: always_put_required_named_parameters_first
// ignore_for_file: constant_identifier_names
// ignore_for_file: lines_longer_than_80_chars

part of openapi.api;


class WebSocketApi {
  WebSocketApi([ApiClient? apiClient]) : apiClient = apiClient ?? defaultApiClient;

  final ApiClient apiClient;

  /// WebSocket connection for real-time updates
  ///
  /// Establishes a WebSocket connection for receiving real-time agent progress events. Clients can subscribe to specific sessions via subscribe messages. 
  ///
  /// Note: This method returns the HTTP [Response].
  ///
  /// Parameters:
  ///
  /// * [String] sessionId:
  ///   Optional session ID to filter events
  Future<Response> getWebSocketWithHttpInfo({ String? sessionId, Future<void>? abortTrigger, }) async {
    // ignore: prefer_const_declarations
    final path = r'/ws';

    // ignore: prefer_final_locals
    Object? postBody;

    final queryParams = <QueryParam>[];
    final headerParams = <String, String>{};
    final formParams = <String, String>{};

    if (sessionId != null) {
      queryParams.addAll(_queryParams('', 'session_id', sessionId));
    }

    const contentTypes = <String>[];


    return apiClient.invokeAPI(
      path,
      'GET',
      queryParams,
      postBody,
      headerParams,
      formParams,
      contentTypes.isEmpty ? null : contentTypes.first,
      abortTrigger: abortTrigger,
    );
  }

  /// WebSocket connection for real-time updates
  ///
  /// Establishes a WebSocket connection for receiving real-time agent progress events. Clients can subscribe to specific sessions via subscribe messages. 
  ///
  /// Parameters:
  ///
  /// * [String] sessionId:
  ///   Optional session ID to filter events
  Future<void> getWebSocket({ String? sessionId, Future<void>? abortTrigger, }) async {
    final response = await getWebSocketWithHttpInfo(sessionId: sessionId, abortTrigger: abortTrigger,);
    if (response.statusCode >= HttpStatus.badRequest) {
      throw ApiException(response.statusCode, await _decodeBodyBytes(response));
    }
  }
}
