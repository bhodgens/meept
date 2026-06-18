import 'package:test/test.dart';
import 'package:meept_client/meept_client.dart';


/// tests for HealthApi
void main() {
  final instance = MeeptClient().getHealthApi();

  group(HealthApi, () {
    // s.handleHealth
    //
    //Future healthGet() async
    test('test healthGet', () async {
      // TODO
    });

  });
}
