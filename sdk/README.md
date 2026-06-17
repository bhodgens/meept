# Meept OpenAPI SDKs

This directory contains generated client SDKs for the Meept HTTP API, generated from the OpenAPI 3.0 specification.

## Generated SDKs

- **Go SDK** (`go/`) - Type-safe Go client for the Meept API
- **Dart SDK** (`dart/`) - Type-safe Dart client for Flutter applications

## Prerequisites

### Java Runtime (Required for Generation)
The OpenAPI Generator requires Java 17+:

```bash
# macOS
brew install openjdk@17
echo 'export PATH="/opt/homebrew/opt/openjdk@17/bin:$PATH"' >> ~/.bash_profile
```

### OpenAPI Generator CLI

```bash
npm install -g @openapitools/openapi-generator-cli
```

## Generation Commands

### Generate All SDKs
```bash
make sdk-generate
```

### Generate Individual SDKs
```bash
# Go SDK only
make sdk-generate-go

# Dart SDK only
make sdk-generate-dart
```

### Clean Generated SDKs
```bash
make sdk-clean
```

### Test SDKs Compile
```bash
make sdk-test
```

## Using the Go SDK

```go
import "github.com/caimlas/meept/sdk/go"

// Create client
config := meeptclient.NewConfiguration()
config.BasePath = "https://localhost:8081"
client := meeptclient.NewAPIClient(config)

// Call API
sessions, _, err := client.V1Api.ListSessions(context.Background()).Execute()
```

## Using the Dart SDK

```dart
import 'package:meept_client/api.dart';

// Create client
final client = Client(
  basePath: 'https://localhost:8081',
  authentication: ApiKeyAuth('Bearer', apiKey: 'your-api-key'),
);

// Call API
final sessions = await client.v1.listSessions();
```

## OpenAPI Spec

The source OpenAPI specification is located at:
`docs/reference/http-api/openapi.yaml`

To regenerate the spec from Go source:
```bash
make docs-generate-openapi
```

## Notes

- SDKs are generated with `--skip-validate-spec` to tolerate minor spec inconsistencies
- Generated code should not be manually edited
- Run `make sdk-generate` after API changes to update SDKs
