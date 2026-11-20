import 'dart:async';
import 'dart:io';

// Hermetic network guard for the whole Flutter test tier (bughunt
// 2026-11-19 wave 3). A LIVE meept-daemon listening on the default stub
// endpoint (localhost:8081) made any test with an unstubbed SDK route
// hit real HTTP with multi-minute timeouts - the suite was not
// hermetic repo-wide. This override refuses every NON-loopback
// connection outright (fail fast, named error) while allowing the
// loopback servers the e2e stub daemon legitimately binds. Tests that
// need HTTP must stub their routes (test/e2e/stub_daemon.dart or a
// Dio adapter fake); an unstubbed remote call now fails in
// milliseconds with a clear message instead of hanging.
//
// flutter_test discovers this file by convention
// (test/flutter_test_config.dart) and wraps every test file's main()
// with testExecutable().
Future<void> testExecutable(FutureOr<void> Function() testMain) async {
  HttpOverrides.global = _HermeticHttpOverrides();
  await testMain();
}

class _HermeticHttpOverrides extends HttpOverrides {
  @override
  HttpClient createHttpClient(SecurityContext? context) {
    return _HermeticHttpClient(super.createHttpClient(context));
  }
}

// Delegating client: every connect/open entry point is checked against
// the loopback allow-list before the real client sees it. Loopback
// names: localhost, 127.0.0.1, ::1 (the stub daemon binds 127.0.0.1).
// All other members forward to the inner client unchanged.
class _HermeticHttpClient implements HttpClient {
  _HermeticHttpClient(this._inner) {
    idleTimeout = _inner.idleTimeout;
  }

  final HttpClient _inner;

  static bool _isLoopback(String host) {
    return host == 'localhost' || host == '127.0.0.1' || host == '::1';
  }

  static void _check(String host) {
    if (!_isLoopback(host)) {
      throw StateError(
        'hermetic test guard: non-loopback HTTP refused ($host). '
        'Stub the route (test/e2e/stub_daemon.dart or a Dio adapter '
        'fake) - real network calls are not allowed in the test tier.',
      );
    }
  }

  // --- guarded connect/open surface ---

  @override
  Future<HttpClientRequest> open(
    String method,
    String host,
    int port,
    String path,
  ) {
    _check(host);
    return _inner.open(method, host, port, path);
  }

  @override
  Future<HttpClientRequest> openUrl(String method, Uri url) {
    _check(url.host);
    return _inner.openUrl(method, url);
  }

  @override
  Future<HttpClientRequest> get(String host, int port, String path) {
    _check(host);
    return _inner.get(host, port, path);
  }

  @override
  Future<HttpClientRequest> getUrl(Uri url) {
    _check(url.host);
    return _inner.getUrl(url);
  }

  @override
  Future<HttpClientRequest> head(String host, int port, String path) {
    _check(host);
    return _inner.head(host, port, path);
  }

  @override
  Future<HttpClientRequest> headUrl(Uri url) {
    _check(url.host);
    return _inner.headUrl(url);
  }

  @override
  Future<HttpClientRequest> post(String host, int port, String path) {
    _check(host);
    return _inner.post(host, port, path);
  }

  @override
  Future<HttpClientRequest> postUrl(Uri url) {
    _check(url.host);
    return _inner.postUrl(url);
  }

  @override
  Future<HttpClientRequest> put(String host, int port, String path) {
    _check(host);
    return _inner.put(host, port, path);
  }

  @override
  Future<HttpClientRequest> putUrl(Uri url) {
    _check(url.host);
    return _inner.putUrl(url);
  }

  @override
  Future<HttpClientRequest> delete(String host, int port, String path) {
    _check(host);
    return _inner.delete(host, port, path);
  }

  @override
  Future<HttpClientRequest> deleteUrl(Uri url) {
    _check(url.host);
    return _inner.deleteUrl(url);
  }

  @override
  Future<HttpClientRequest> patch(String host, int port, String path) {
    _check(host);
    return _inner.patch(host, port, path);
  }

  @override
  Future<HttpClientRequest> patchUrl(Uri url) {
    _check(url.host);
    return _inner.patchUrl(url);
  }

  // --- pure delegation (no host argument to check) ---

  @override
  late Duration idleTimeout;

  @override
  Duration? get connectionTimeout => _inner.connectionTimeout;
  @override
  set connectionTimeout(Duration? value) => _inner.connectionTimeout = value;

  @override
  int? get maxConnectionsPerHost => _inner.maxConnectionsPerHost;
  @override
  set maxConnectionsPerHost(int? value) => _inner.maxConnectionsPerHost = value;

  @override
  bool get autoUncompress => _inner.autoUncompress;
  @override
  set autoUncompress(bool value) => _inner.autoUncompress = value;

  @override
  String? get userAgent => _inner.userAgent;
  @override
  set userAgent(String? value) => _inner.userAgent = value;

  @override
  void addCredentials(
    Uri url,
    String realm,
    HttpClientCredentials credentials,
  ) => _inner.addCredentials(url, realm, credentials);

  @override
  void addProxyCredentials(
    String host,
    int port,
    String realm,
    HttpClientCredentials credentials,
  ) => _inner.addProxyCredentials(host, port, realm, credentials);

  @override
  set authenticate(
    Future<bool> Function(Uri url, String scheme, String? realm)? f,
  ) => _inner.authenticate = f;

  @override
  set authenticateProxy(
    Future<bool> Function(String host, int port, String scheme, String? realm)?
    f,
  ) => _inner.authenticateProxy = f;

  @override
  set badCertificateCallback(
    bool Function(X509Certificate cert, String host, int port)? callback,
  ) => _inner.badCertificateCallback = callback;

  @override
  set connectionFactory(
    Future<ConnectionTask<Socket>> Function(
      Uri url,
      String? proxyHost,
      int? proxyPort,
    )?
    f,
  ) => _inner.connectionFactory = f;

  @override
  set findProxy(String Function(Uri url)? f) => _inner.findProxy = f;

  @override
  set keyLog(void Function(String line)? callback) => _inner.keyLog = callback;

  @override
  void close({bool force = false}) => _inner.close(force: force);
}
