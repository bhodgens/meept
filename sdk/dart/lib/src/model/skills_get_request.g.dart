// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'skills_get_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SkillsGetRequest extends SkillsGetRequest {
  @override
  final String slug;

  factory _$SkillsGetRequest(
          [void Function(SkillsGetRequestBuilder)? updates]) =>
      (SkillsGetRequestBuilder()..update(updates))._build();

  _$SkillsGetRequest._({required this.slug}) : super._();
  @override
  SkillsGetRequest rebuild(void Function(SkillsGetRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SkillsGetRequestBuilder toBuilder() =>
      SkillsGetRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SkillsGetRequest && slug == other.slug;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, slug.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SkillsGetRequest')..add('slug', slug))
        .toString();
  }
}

class SkillsGetRequestBuilder
    implements Builder<SkillsGetRequest, SkillsGetRequestBuilder> {
  _$SkillsGetRequest? _$v;

  String? _slug;
  String? get slug => _$this._slug;
  set slug(String? slug) => _$this._slug = slug;

  SkillsGetRequestBuilder() {
    SkillsGetRequest._defaults(this);
  }

  SkillsGetRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _slug = $v.slug;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SkillsGetRequest other) {
    _$v = other as _$SkillsGetRequest;
  }

  @override
  void update(void Function(SkillsGetRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SkillsGetRequest build() => _build();

  _$SkillsGetRequest _build() {
    final _$result = _$v ??
        _$SkillsGetRequest._(
          slug: BuiltValueNullFieldError.checkNotNull(
              slug, r'SkillsGetRequest', 'slug'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
