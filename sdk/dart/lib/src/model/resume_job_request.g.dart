// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'resume_job_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ResumeJobRequest extends ResumeJobRequest {
  @override
  final String id;

  factory _$ResumeJobRequest(
          [void Function(ResumeJobRequestBuilder)? updates]) =>
      (ResumeJobRequestBuilder()..update(updates))._build();

  _$ResumeJobRequest._({required this.id}) : super._();
  @override
  ResumeJobRequest rebuild(void Function(ResumeJobRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ResumeJobRequestBuilder toBuilder() =>
      ResumeJobRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ResumeJobRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'ResumeJobRequest')..add('id', id))
        .toString();
  }
}

class ResumeJobRequestBuilder
    implements Builder<ResumeJobRequest, ResumeJobRequestBuilder> {
  _$ResumeJobRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  ResumeJobRequestBuilder() {
    ResumeJobRequest._defaults(this);
  }

  ResumeJobRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ResumeJobRequest other) {
    _$v = other as _$ResumeJobRequest;
  }

  @override
  void update(void Function(ResumeJobRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ResumeJobRequest build() => _build();

  _$ResumeJobRequest _build() {
    final _$result = _$v ??
        _$ResumeJobRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'ResumeJobRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
