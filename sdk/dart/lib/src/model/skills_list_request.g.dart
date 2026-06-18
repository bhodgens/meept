// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'skills_list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$SkillsListRequest extends SkillsListRequest {
  @override
  final String? categoryCommaOmitempty;
  @override
  final int? limitCommaOmitempty;

  factory _$SkillsListRequest(
          [void Function(SkillsListRequestBuilder)? updates]) =>
      (SkillsListRequestBuilder()..update(updates))._build();

  _$SkillsListRequest._({this.categoryCommaOmitempty, this.limitCommaOmitempty})
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
        categoryCommaOmitempty == other.categoryCommaOmitempty &&
        limitCommaOmitempty == other.limitCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, categoryCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'SkillsListRequest')
          ..add('categoryCommaOmitempty', categoryCommaOmitempty)
          ..add('limitCommaOmitempty', limitCommaOmitempty))
        .toString();
  }
}

class SkillsListRequestBuilder
    implements Builder<SkillsListRequest, SkillsListRequestBuilder> {
  _$SkillsListRequest? _$v;

  String? _categoryCommaOmitempty;
  String? get categoryCommaOmitempty => _$this._categoryCommaOmitempty;
  set categoryCommaOmitempty(String? categoryCommaOmitempty) =>
      _$this._categoryCommaOmitempty = categoryCommaOmitempty;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  SkillsListRequestBuilder() {
    SkillsListRequest._defaults(this);
  }

  SkillsListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _categoryCommaOmitempty = $v.categoryCommaOmitempty;
      _limitCommaOmitempty = $v.limitCommaOmitempty;
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
          categoryCommaOmitempty: categoryCommaOmitempty,
          limitCommaOmitempty: limitCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
