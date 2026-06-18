// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'task_list_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$TaskListRequest extends TaskListRequest {
  @override
  final int? limitCommaOmitempty;
  @override
  final String? sessionIdCommaOmitempty;

  factory _$TaskListRequest([void Function(TaskListRequestBuilder)? updates]) =>
      (TaskListRequestBuilder()..update(updates))._build();

  _$TaskListRequest._({this.limitCommaOmitempty, this.sessionIdCommaOmitempty})
      : super._();
  @override
  TaskListRequest rebuild(void Function(TaskListRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  TaskListRequestBuilder toBuilder() => TaskListRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is TaskListRequest &&
        limitCommaOmitempty == other.limitCommaOmitempty &&
        sessionIdCommaOmitempty == other.sessionIdCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, limitCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, sessionIdCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'TaskListRequest')
          ..add('limitCommaOmitempty', limitCommaOmitempty)
          ..add('sessionIdCommaOmitempty', sessionIdCommaOmitempty))
        .toString();
  }
}

class TaskListRequestBuilder
    implements Builder<TaskListRequest, TaskListRequestBuilder> {
  _$TaskListRequest? _$v;

  int? _limitCommaOmitempty;
  int? get limitCommaOmitempty => _$this._limitCommaOmitempty;
  set limitCommaOmitempty(int? limitCommaOmitempty) =>
      _$this._limitCommaOmitempty = limitCommaOmitempty;

  String? _sessionIdCommaOmitempty;
  String? get sessionIdCommaOmitempty => _$this._sessionIdCommaOmitempty;
  set sessionIdCommaOmitempty(String? sessionIdCommaOmitempty) =>
      _$this._sessionIdCommaOmitempty = sessionIdCommaOmitempty;

  TaskListRequestBuilder() {
    TaskListRequest._defaults(this);
  }

  TaskListRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _limitCommaOmitempty = $v.limitCommaOmitempty;
      _sessionIdCommaOmitempty = $v.sessionIdCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(TaskListRequest other) {
    _$v = other as _$TaskListRequest;
  }

  @override
  void update(void Function(TaskListRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  TaskListRequest build() => _build();

  _$TaskListRequest _build() {
    final _$result = _$v ??
        _$TaskListRequest._(
          limitCommaOmitempty: limitCommaOmitempty,
          sessionIdCommaOmitempty: sessionIdCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
