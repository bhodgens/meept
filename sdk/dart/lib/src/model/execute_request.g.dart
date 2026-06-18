// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'execute_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ExecuteRequest extends ExecuteRequest {
  @override
  final String slug;
  @override
  final String prompt;

  factory _$ExecuteRequest([void Function(ExecuteRequestBuilder)? updates]) =>
      (ExecuteRequestBuilder()..update(updates))._build();

  _$ExecuteRequest._({required this.slug, required this.prompt}) : super._();
  @override
  ExecuteRequest rebuild(void Function(ExecuteRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ExecuteRequestBuilder toBuilder() => ExecuteRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ExecuteRequest &&
        slug == other.slug &&
        prompt == other.prompt;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, slug.hashCode);
    _$hash = $jc(_$hash, prompt.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ExecuteRequest')
          ..add('slug', slug)
          ..add('prompt', prompt))
        .toString();
  }
}

class ExecuteRequestBuilder
    implements Builder<ExecuteRequest, ExecuteRequestBuilder> {
  _$ExecuteRequest? _$v;

  String? _slug;
  String? get slug => _$this._slug;
  set slug(String? slug) => _$this._slug = slug;

  String? _prompt;
  String? get prompt => _$this._prompt;
  set prompt(String? prompt) => _$this._prompt = prompt;

  ExecuteRequestBuilder() {
    ExecuteRequest._defaults(this);
  }

  ExecuteRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _slug = $v.slug;
      _prompt = $v.prompt;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ExecuteRequest other) {
    _$v = other as _$ExecuteRequest;
  }

  @override
  void update(void Function(ExecuteRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ExecuteRequest build() => _build();

  _$ExecuteRequest _build() {
    final _$result = _$v ??
        _$ExecuteRequest._(
          slug: BuiltValueNullFieldError.checkNotNull(
              slug, r'ExecuteRequest', 'slug'),
          prompt: BuiltValueNullFieldError.checkNotNull(
              prompt, r'ExecuteRequest', 'prompt'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
