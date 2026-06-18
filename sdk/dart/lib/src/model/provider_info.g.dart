// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'provider_info.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ProviderInfo extends ProviderInfo {
  @override
  final String id;
  @override
  final String name;
  @override
  final String api;
  @override
  final String baseUrl;
  @override
  final String? models;
  @override
  final bool hasCredentials;

  factory _$ProviderInfo([void Function(ProviderInfoBuilder)? updates]) =>
      (ProviderInfoBuilder()..update(updates))._build();

  _$ProviderInfo._(
      {required this.id,
      required this.name,
      required this.api,
      required this.baseUrl,
      this.models,
      required this.hasCredentials})
      : super._();
  @override
  ProviderInfo rebuild(void Function(ProviderInfoBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ProviderInfoBuilder toBuilder() => ProviderInfoBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ProviderInfo &&
        id == other.id &&
        name == other.name &&
        api == other.api &&
        baseUrl == other.baseUrl &&
        models == other.models &&
        hasCredentials == other.hasCredentials;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, api.hashCode);
    _$hash = $jc(_$hash, baseUrl.hashCode);
    _$hash = $jc(_$hash, models.hashCode);
    _$hash = $jc(_$hash, hasCredentials.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'ProviderInfo')
          ..add('id', id)
          ..add('name', name)
          ..add('api', api)
          ..add('baseUrl', baseUrl)
          ..add('models', models)
          ..add('hasCredentials', hasCredentials))
        .toString();
  }
}

class ProviderInfoBuilder
    implements Builder<ProviderInfo, ProviderInfoBuilder> {
  _$ProviderInfo? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _api;
  String? get api => _$this._api;
  set api(String? api) => _$this._api = api;

  String? _baseUrl;
  String? get baseUrl => _$this._baseUrl;
  set baseUrl(String? baseUrl) => _$this._baseUrl = baseUrl;

  String? _models;
  String? get models => _$this._models;
  set models(String? models) => _$this._models = models;

  bool? _hasCredentials;
  bool? get hasCredentials => _$this._hasCredentials;
  set hasCredentials(bool? hasCredentials) =>
      _$this._hasCredentials = hasCredentials;

  ProviderInfoBuilder() {
    ProviderInfo._defaults(this);
  }

  ProviderInfoBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _name = $v.name;
      _api = $v.api;
      _baseUrl = $v.baseUrl;
      _models = $v.models;
      _hasCredentials = $v.hasCredentials;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ProviderInfo other) {
    _$v = other as _$ProviderInfo;
  }

  @override
  void update(void Function(ProviderInfoBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ProviderInfo build() => _build();

  _$ProviderInfo _build() {
    final _$result = _$v ??
        _$ProviderInfo._(
          id: BuiltValueNullFieldError.checkNotNull(id, r'ProviderInfo', 'id'),
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'ProviderInfo', 'name'),
          api: BuiltValueNullFieldError.checkNotNull(
              api, r'ProviderInfo', 'api'),
          baseUrl: BuiltValueNullFieldError.checkNotNull(
              baseUrl, r'ProviderInfo', 'baseUrl'),
          models: models,
          hasCredentials: BuiltValueNullFieldError.checkNotNull(
              hasCredentials, r'ProviderInfo', 'hasCredentials'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
