// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'audit_entry.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AuditEntry extends AuditEntry {
  @override
  final String timestamp;
  @override
  final String action;
  @override
  final String resource;
  @override
  final bool allowed;

  factory _$AuditEntry([void Function(AuditEntryBuilder)? updates]) =>
      (AuditEntryBuilder()..update(updates))._build();

  _$AuditEntry._(
      {required this.timestamp,
      required this.action,
      required this.resource,
      required this.allowed})
      : super._();
  @override
  AuditEntry rebuild(void Function(AuditEntryBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AuditEntryBuilder toBuilder() => AuditEntryBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AuditEntry &&
        timestamp == other.timestamp &&
        action == other.action &&
        resource == other.resource &&
        allowed == other.allowed;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, timestamp.hashCode);
    _$hash = $jc(_$hash, action.hashCode);
    _$hash = $jc(_$hash, resource.hashCode);
    _$hash = $jc(_$hash, allowed.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AuditEntry')
          ..add('timestamp', timestamp)
          ..add('action', action)
          ..add('resource', resource)
          ..add('allowed', allowed))
        .toString();
  }
}

class AuditEntryBuilder implements Builder<AuditEntry, AuditEntryBuilder> {
  _$AuditEntry? _$v;

  String? _timestamp;
  String? get timestamp => _$this._timestamp;
  set timestamp(String? timestamp) => _$this._timestamp = timestamp;

  String? _action;
  String? get action => _$this._action;
  set action(String? action) => _$this._action = action;

  String? _resource;
  String? get resource => _$this._resource;
  set resource(String? resource) => _$this._resource = resource;

  bool? _allowed;
  bool? get allowed => _$this._allowed;
  set allowed(bool? allowed) => _$this._allowed = allowed;

  AuditEntryBuilder() {
    AuditEntry._defaults(this);
  }

  AuditEntryBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _timestamp = $v.timestamp;
      _action = $v.action;
      _resource = $v.resource;
      _allowed = $v.allowed;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AuditEntry other) {
    _$v = other as _$AuditEntry;
  }

  @override
  void update(void Function(AuditEntryBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AuditEntry build() => _build();

  _$AuditEntry _build() {
    final _$result = _$v ??
        _$AuditEntry._(
          timestamp: BuiltValueNullFieldError.checkNotNull(
              timestamp, r'AuditEntry', 'timestamp'),
          action: BuiltValueNullFieldError.checkNotNull(
              action, r'AuditEntry', 'action'),
          resource: BuiltValueNullFieldError.checkNotNull(
              resource, r'AuditEntry', 'resource'),
          allowed: BuiltValueNullFieldError.checkNotNull(
              allowed, r'AuditEntry', 'allowed'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
