// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'attendee_info.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AttendeeInfo extends AttendeeInfo {
  @override
  final String email;
  @override
  final String? displayName;
  @override
  final String? response;

  factory _$AttendeeInfo([void Function(AttendeeInfoBuilder)? updates]) =>
      (AttendeeInfoBuilder()..update(updates))._build();

  _$AttendeeInfo._(
      {required this.email,
      this.displayName,
      this.response})
      : super._();
  @override
  AttendeeInfo rebuild(void Function(AttendeeInfoBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  AttendeeInfoBuilder toBuilder() => AttendeeInfoBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is AttendeeInfo &&
        email == other.email &&
        displayName == other.displayName &&
        response == other.response;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, email.hashCode);
    _$hash = $jc(_$hash, displayName.hashCode);
    _$hash = $jc(_$hash, response.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AttendeeInfo')
          ..add('email', email)
          ..add('displayName', displayName)
          ..add('response', response))
        .toString();
  }
}

class AttendeeInfoBuilder
    implements Builder<AttendeeInfo, AttendeeInfoBuilder> {
  _$AttendeeInfo? _$v;

  String? _email;
  String? get email => _$this._email;
  set email(String? email) => _$this._email = email;

  String? _displayName;
  String? get displayName => _$this._displayName;
  set displayName(String? displayName) =>
      _$this._displayName = displayName;

  String? _response;
  String? get response => _$this._response;
  set response(String? response) =>
      _$this._response = response;

  AttendeeInfoBuilder() {
    AttendeeInfo._defaults(this);
  }

  AttendeeInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _email = $v.email;
      _displayName = $v.displayName;
      _response = $v.response;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(AttendeeInfo other) {
    _$v = other as _$AttendeeInfo;
  }

  @override
  void update(void Function(AttendeeInfoBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  AttendeeInfo build() => _build();

  _$AttendeeInfo _build() {
    final _$result = _$v ??
        _$AttendeeInfo._(
          email: BuiltValueNullFieldError.checkNotNull(
              email, r'AttendeeInfo', 'email'),
          displayName: displayName,
          response: response,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
