# Dart HTTP Query Battery — Grounded Expected Symbols

All symbols verified against dart-lang/http source on GitHub (2026-05-19).

## Investigation 1: Client and request lifecycle (9 symbols)

Query keyword: `"Client BaseClient BaseRequest finalize send headers persistent"`
Query intent: `"how does Dart http construct finalize and send requests through the client interface"`

Expected symbols:
- `Client` — pkgs/http/lib/src/client.dart — abstract interface defining HTTP helpers, `send`, `close`, and platform-adaptive client construction
- `BaseClient` — pkgs/http/lib/src/base_client.dart — base implementation of convenience methods in terms of `send`
- `BaseRequest` — pkgs/http/lib/src/base_request.dart — mutable request base class with headers, redirects, persistence, and finalization state
- `Request` — pkgs/http/lib/src/request.dart — concrete buffered request with body, body bytes, fields, and encoding helpers
- `AbortableRequest` — pkgs/http/lib/src/request.dart — request variant that supports cancellation through an abort trigger
- `runWithClient` — pkgs/http/lib/src/client.dart — runs a callback in a zone whose default HTTP client is overridden
- `ClientException` — pkgs/http/lib/src/exception.dart — base transport exception type carrying the failed URI
- `finalize` — pkgs/http/lib/src/base_request.dart — freezes mutable request fields and returns the outgoing body byte stream
- `fromStream` — pkgs/http/lib/src/response.dart — drains a streamed response into a buffered `Response`

## Investigation 2: Streamed request and response pipeline (8 symbols)

Query keyword: `"StreamedRequest StreamedResponse ByteStream sink pipe chunk"`
Query intent: `"how does Dart http stream request bodies and consume response bodies incrementally"`

Expected symbols:
- `StreamedRequest` — pkgs/http/lib/src/streamed_request.dart — request whose body is written asynchronously to a stream sink
- `sink` — pkgs/http/lib/src/streamed_request.dart — sink getter used by callers to write outgoing request body chunks
- `AbortableStreamedRequest` — pkgs/http/lib/src/streamed_request.dart — streamed request variant that can be aborted while sending or receiving
- `StreamedResponse` — pkgs/http/lib/src/streamed_response.dart — response whose body is exposed as a byte stream
- `ByteStream` — pkgs/http/lib/src/byte_stream.dart — canonical byte-chunk stream wrapper used throughout the package
- `toBytes` — pkgs/http/lib/src/byte_stream.dart — collects all byte chunks into a single `Uint8List`
- `fromBytes` — pkgs/http/lib/src/byte_stream.dart — creates a single-emission byte stream from an in-memory byte list
- `IOStreamedResponse` — pkgs/http/lib/src/io_streamed_response.dart — streamed response returned by the `dart:io` client with socket-detach support

## Investigation 3: Multipart form-data uploads (8 symbols)

Query keyword: `"MultipartRequest MultipartFile boundary form-data MIME MediaType upload"`
Query intent: `"how does Dart http build multipart form uploads with fields files boundaries and content types"`

Expected symbols:
- `MultipartRequest` — pkgs/http/lib/src/multipart_request.dart — multipart form-data request that streams text fields and binary files
- `AbortableMultipartRequest` — pkgs/http/lib/src/multipart_request.dart — multipart request variant that supports abort cancellation
- `MultipartFile` — pkgs/http/lib/src/multipart_file.dart — representation of one multipart binary part with filename, length, content type, and byte stream
- `fromBytes` — pkgs/http/lib/src/multipart_file.dart — creates a multipart file from an in-memory byte list
- `fromString` — pkgs/http/lib/src/multipart_file.dart — creates a multipart file from text using charset-aware encoding
- `fromPath` — pkgs/http/lib/src/multipart_file.dart — creates a multipart file by reading a file from disk
- `multipartFileFromPath` — pkgs/http/lib/src/multipart_file_io.dart — `dart:io` implementation that stats and opens a file stream for multipart upload
- `MediaType` — pkgs/http_parser/lib/src/media_type.dart — immutable MIME media type with parameters used for multipart content headers

## Investigation 4: Browser and IO client implementations (8 symbols)

Query keyword: `"BrowserClient IOClient fetch dart:io credentials redirect headers"`
Query intent: `"how does Dart http choose browser or IO clients and expose response metadata"`

Expected symbols:
- `BrowserClient` — pkgs/http/lib/src/browser_client.dart — browser/Wasm client backed by the Fetch API
- `withCredentials` — pkgs/http/lib/src/browser_client.dart — controls whether browser requests include cross-origin credentials
- `IOClient` — pkgs/http/lib/src/io_client.dart — native/server client backed by `dart:io` `HttpClient`
- `send` — pkgs/http/lib/src/io_client.dart — opens a native HTTP connection, pipes request bytes, handles aborts, and returns a streamed response
- `createClient` — pkgs/http/lib/src/io_client.dart — platform factory used by conditional imports to create the default client
- `BaseResponse` — pkgs/http/lib/src/base_response.dart — shared response metadata base class
- `BaseResponseWithUrl` — pkgs/http/lib/src/base_response.dart — interface for responses that expose the final URL after redirects
- `HeadersWithSplitValues` — pkgs/http/lib/src/base_response.dart — extension that exposes split header values with cookie-aware parsing

## Investigation 5: Retry, mock client, and abort utilities (9 symbols)

Query keyword: `"RetryClient MockClient retry delay whenError abortTrigger mock"`
Query intent: `"how does Dart http retry failed requests and mock clients in tests without real network calls"`

Expected symbols:
- `RetryClient` — pkgs/http/lib/retry.dart — client wrapper that retries failed responses or errors with configurable conditions and delays
- `withDelays` — pkgs/http/lib/retry.dart — retry client constructor using explicit retry delay durations
- `MockClient` — pkgs/http/lib/src/mock_client.dart — test client that routes requests through local handlers instead of network I/O
- `streaming` — pkgs/http/lib/src/mock_client.dart — mock client constructor for streaming request and response tests
- `pngResponse` — pkgs/http/lib/src/mock_client.dart — helper response containing a tiny PNG image payload
- `MockClientHandler` — pkgs/http/lib/src/mock_client.dart — typedef for non-streaming mock request handlers
- `MockClientStreamHandler` — pkgs/http/lib/src/mock_client.dart — typedef for streaming mock handlers
- `Abortable` — pkgs/http/lib/src/abortable.dart — mixin contract for requests that can be cancelled through an abort trigger
- `RequestAbortedException` — pkgs/http/lib/src/abortable.dart — client exception used when an abortable request is cancelled
