// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'templates_clear_result.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TemplatesClearResult extends TemplatesClearResult {
  @override
  final String? cleared;

  factory _$TemplatesClearResult(
          [void Function(TemplatesClearResultBuilder)? updates]) =>
      (TemplatesClearResultBuilder()..update(updates))._build();

  _$TemplatesClearResult._({this.cleared}) : super._();
  @override
  TemplatesClearResult rebuild(
          void Function(TemplatesClearResultBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TemplatesClearResultBuilder toBuilder() =>
      TemplatesClearResultBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TemplatesClearResult && cleared == other.cleared;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, cleared.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TemplatesClearResult')
          ..add('cleared', cleared))
        .toString();
  }
}

class TemplatesClearResultBuilder
    implements Builder<TemplatesClearResult, TemplatesClearResultBuilder> {
  _$TemplatesClearResult? _$v;

  String? _cleared;
  String? get cleared => _$this._cleared;
  set cleared(String? cleared) => _$this._cleared = cleared;

  TemplatesClearResultBuilder() {
    TemplatesClearResult._defaults(this);
  }

  TemplatesClearResultBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _cleared = $v.cleared;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TemplatesClearResult other) {
    _$v = other as _$TemplatesClearResult;
  }

  @override
  void update(void Function(TemplatesClearResultBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TemplatesClearResult build() => _build();

  _$TemplatesClearResult _build() {
    final _$result = _$v ??
        _$TemplatesClearResult._(
          cleared: cleared,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
