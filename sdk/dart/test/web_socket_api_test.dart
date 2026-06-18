import 'package:test/test.dart';
import 'package:meept_client/meept_client.dart';


/// tests for WebSocketApi
void main() {
  final instance = MeeptClient().getWebSocketApi();

  group(WebSocketApi, () {
    // WebSocket connection for real-time updates
    //
    // Establishes a WebSocket connection for receiving real-time agent progress events. Clients can subscribe to specific sessions via subscribe messages. 
    //
    //Future getWebSocket({ String sessionId }) async
    test('test getWebSocket', () async {
      // TODO
    });

  });
}
