// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'trigger_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TriggerRequest extends TriggerRequest {
  @override
  final bool? forceCommaOmitempty;

  factory _$TriggerRequest([void Function(TriggerRequestBuilder)? updates]) =>
      (TriggerRequestBuilder()..update(updates))._build();

  _$TriggerRequest._({this.forceCommaOmitempty}) : super._();
  @override
  TriggerRequest rebuild(void Function(TriggerRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TriggerRequestBuilder toBuilder() => TriggerRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TriggerRequest &&
        forceCommaOmitempty == other.forceCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, forceCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TriggerRequest')
          ..add('forceCommaOmitempty', forceCommaOmitempty))
        .toString();
  }
}

class TriggerRequestBuilder
    implements Builder<TriggerRequest, TriggerRequestBuilder> {
  _$TriggerRequest? _$v;

  bool? _forceCommaOmitempty;
  bool? get forceCommaOmitempty => _$this._forceCommaOmitempty;
  set forceCommaOmitempty(bool? forceCommaOmitempty) =>
      _$this._forceCommaOmitempty = forceCommaOmitempty;

  TriggerRequestBuilder() {
    TriggerRequest._defaults(this);
  }

  TriggerRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _forceCommaOmitempty = $v.forceCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TriggerRequest other) {
    _$v = other as _$TriggerRequest;
  }

  @override
  void update(void Function(TriggerRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TriggerRequest build() => _build();

  _$TriggerRequest _build() {
    final _$result = _$v ??
        _$TriggerRequest._(
          forceCommaOmitempty: forceCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
