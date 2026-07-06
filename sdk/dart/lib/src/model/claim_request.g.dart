// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'claim_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ClaimRequest extends ClaimRequest {
  @override
  final String workerId;
  @override
  final String? capabilities;

  factory _$ClaimRequest([void Function(ClaimRequestBuilder)? updates]) =>
      (ClaimRequestBuilder()..update(updates))._build();

  _$ClaimRequest._({required this.workerId, this.capabilities})
      : super._();
  @override
  ClaimRequest rebuild(void Function(ClaimRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ClaimRequestBuilder toBuilder() => ClaimRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ClaimRequest &&
        workerId == other.workerId &&
        capabilities == other.capabilities;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, workerId.hashCode);
    _$hash = $jc(_$hash, capabilities.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ClaimRequest')
          ..add('workerId', workerId)
          ..add('capabilities', capabilities))
        .toString();
  }
}

class ClaimRequestBuilder
    implements Builder<ClaimRequest, ClaimRequestBuilder> {
  _$ClaimRequest? _$v;

  String? _workerId;
  String? get workerId => _$this._workerId;
  set workerId(String? workerId) => _$this._workerId = workerId;

  String? _capabilities;
  String? get capabilities => _$this._capabilities;
  set capabilities(String? capabilities) =>
      _$this._capabilities = capabilities;

  ClaimRequestBuilder() {
    ClaimRequest._defaults(this);
  }

  ClaimRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _workerId = $v.workerId;
      _capabilities = $v.capabilities;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ClaimRequest other) {
    _$v = other as _$ClaimRequest;
  }

  @override
  void update(void Function(ClaimRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ClaimRequest build() => _build();

  _$ClaimRequest _build() {
    final _$result = _$v ??
        _$ClaimRequest._(
          workerId: BuiltValueNullFieldError.checkNotNull(
              workerId, r'ClaimRequest', 'workerId'),
          capabilities: capabilities,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
