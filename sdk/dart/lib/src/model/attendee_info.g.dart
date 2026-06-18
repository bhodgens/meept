// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'attendee_info.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$AttendeeInfo extends AttendeeInfo {
  @override
  final String email;
  @override
  final String? displayNameCommaOmitempty;
  @override
  final String? responseCommaOmitempty;

  factory _$AttendeeInfo([void Function(AttendeeInfoBuilder)? updates]) =>
      (AttendeeInfoBuilder()..update(updates))._build();

  _$AttendeeInfo._(
      {required this.email,
      this.displayNameCommaOmitempty,
      this.responseCommaOmitempty})
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
        displayNameCommaOmitempty == other.displayNameCommaOmitempty &&
        responseCommaOmitempty == other.responseCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, email.hashCode);
    _$hash = $jc(_$hash, displayNameCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, responseCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'AttendeeInfo')
          ..add('email', email)
          ..add('displayNameCommaOmitempty', displayNameCommaOmitempty)
          ..add('responseCommaOmitempty', responseCommaOmitempty))
        .toString();
  }
}

class AttendeeInfoBuilder
    implements Builder<AttendeeInfo, AttendeeInfoBuilder> {
  _$AttendeeInfo? _$v;

  String? _email;
  String? get email => _$this._email;
  set email(String? email) => _$this._email = email;

  String? _displayNameCommaOmitempty;
  String? get displayNameCommaOmitempty => _$this._displayNameCommaOmitempty;
  set displayNameCommaOmitempty(String? displayNameCommaOmitempty) =>
      _$this._displayNameCommaOmitempty = displayNameCommaOmitempty;

  String? _responseCommaOmitempty;
  String? get responseCommaOmitempty => _$this._responseCommaOmitempty;
  set responseCommaOmitempty(String? responseCommaOmitempty) =>
      _$this._responseCommaOmitempty = responseCommaOmitempty;

  AttendeeInfoBuilder() {
    AttendeeInfo._defaults(this);
  }

  AttendeeInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _email = $v.email;
      _displayNameCommaOmitempty = $v.displayNameCommaOmitempty;
      _responseCommaOmitempty = $v.responseCommaOmitempty;
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
          displayNameCommaOmitempty: displayNameCommaOmitempty,
          responseCommaOmitempty: responseCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
