// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'skills_list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SkillsListRequest extends SkillsListRequest {
  @override
  final String? category;
  @override
  final int? limit;

  factory _$SkillsListRequest(
          [void Function(SkillsListRequestBuilder)? updates]) =>
      (SkillsListRequestBuilder()..update(updates))._build();

  _$SkillsListRequest._({this.category, this.limit})
      : super._();
  @override
  SkillsListRequest rebuild(void Function(SkillsListRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  SkillsListRequestBuilder toBuilder() =>
      SkillsListRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is SkillsListRequest &&
        category == other.category &&
        limit == other.limit;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, category.hashCode);
    _$hash = $jc(_$hash, limit.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SkillsListRequest')
          ..add('category', category)
          ..add('limit', limit))
        .toString();
  }
}

class SkillsListRequestBuilder
    implements Builder<SkillsListRequest, SkillsListRequestBuilder> {
  _$SkillsListRequest? _$v;

  String? _category;
  String? get category => _$this._category;
  set category(String? category) =>
      _$this._category = category;

  int? _limit;
  int? get limit => _$this._limit;
  set limit(int? limit) =>
      _$this._limit = limit;

  SkillsListRequestBuilder() {
    SkillsListRequest._defaults(this);
  }

  SkillsListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _category = $v.category;
      _limit = $v.limit;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(SkillsListRequest other) {
    _$v = other as _$SkillsListRequest;
  }

  @override
  void update(void Function(SkillsListRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  SkillsListRequest build() => _build();

  _$SkillsListRequest _build() {
    final _$result = _$v ??
        _$SkillsListRequest._(
          category: category,
          limit: limit,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
