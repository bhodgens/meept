// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'pause_job_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$PauseJobRequest extends PauseJobRequest {
  @override
  final String id;

  factory _$PauseJobRequest([void Function(PauseJobRequestBuilder)? updates]) =>
      (PauseJobRequestBuilder()..update(updates))._build();

  _$PauseJobRequest._({required this.id}) : super._();
  @override
  PauseJobRequest rebuild(void Function(PauseJobRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  PauseJobRequestBuilder toBuilder() => PauseJobRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is PauseJobRequest && id == other.id;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'PauseJobRequest')..add('id', id))
        .toString();
  }
}

class PauseJobRequestBuilder
    implements Builder<PauseJobRequest, PauseJobRequestBuilder> {
  _$PauseJobRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  PauseJobRequestBuilder() {
    PauseJobRequest._defaults(this);
  }

  PauseJobRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(PauseJobRequest other) {
    _$v = other as _$PauseJobRequest;
  }

  @override
  void update(void Function(PauseJobRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  PauseJobRequest build() => _build();

  _$PauseJobRequest _build() {
    final _$result = _$v ??
        _$PauseJobRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'PauseJobRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
