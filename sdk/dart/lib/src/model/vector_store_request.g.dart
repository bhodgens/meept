// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'vector_store_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$VectorStoreRequest extends VectorStoreRequest {
  @override
  final String memoryId;
  @override
  final String content;
  @override
  final String? metadataCommaOmitempty;

  factory _$VectorStoreRequest(
          [void Function(VectorStoreRequestBuilder)? updates]) =>
      (VectorStoreRequestBuilder()..update(updates))._build();

  _$VectorStoreRequest._(
      {required this.memoryId,
      required this.content,
      this.metadataCommaOmitempty})
      : super._();
  @override
  VectorStoreRequest rebuild(
          void Function(VectorStoreRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  VectorStoreRequestBuilder toBuilder() =>
      VectorStoreRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is VectorStoreRequest &&
        memoryId == other.memoryId &&
        content == other.content &&
        metadataCommaOmitempty == other.metadataCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, memoryId.hashCode);
    _$hash = $jc(_$hash, content.hashCode);
    _$hash = $jc(_$hash, metadataCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'VectorStoreRequest')
          ..add('memoryId', memoryId)
          ..add('content', content)
          ..add('metadataCommaOmitempty', metadataCommaOmitempty))
        .toString();
  }
}

class VectorStoreRequestBuilder
    implements Builder<VectorStoreRequest, VectorStoreRequestBuilder> {
  _$VectorStoreRequest? _$v;

  String? _memoryId;
  String? get memoryId => _$this._memoryId;
  set memoryId(String? memoryId) => _$this._memoryId = memoryId;

  String? _content;
  String? get content => _$this._content;
  set content(String? content) => _$this._content = content;

  String? _metadataCommaOmitempty;
  String? get metadataCommaOmitempty => _$this._metadataCommaOmitempty;
  set metadataCommaOmitempty(String? metadataCommaOmitempty) =>
      _$this._metadataCommaOmitempty = metadataCommaOmitempty;

  VectorStoreRequestBuilder() {
    VectorStoreRequest._defaults(this);
  }

  VectorStoreRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _memoryId = $v.memoryId;
      _content = $v.content;
      _metadataCommaOmitempty = $v.metadataCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(VectorStoreRequest other) {
    _$v = other as _$VectorStoreRequest;
  }

  @override
  void update(void Function(VectorStoreRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  VectorStoreRequest build() => _build();

  _$VectorStoreRequest _build() {
    final _$result = _$v ??
        _$VectorStoreRequest._(
          memoryId: BuiltValueNullFieldError.checkNotNull(
              memoryId, r'VectorStoreRequest', 'memoryId'),
          content: BuiltValueNullFieldError.checkNotNull(
              content, r'VectorStoreRequest', 'content'),
          metadataCommaOmitempty: metadataCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
