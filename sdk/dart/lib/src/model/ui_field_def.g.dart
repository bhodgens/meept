// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'ui_field_def.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$UIFieldDef extends UIFieldDef {
  @override
  final String name;
  @override
  final String label;
  @override
  final String type;
  @override
  final bool? requiredCommaOmitempty;
  @override
  final JsonObject? defaultCommaOmitempty;
  @override
  final String? optionsCommaOmitempty;
  @override
  final String? placeholderCommaOmitempty;
  @override
  final String? helpCommaOmitempty;

  factory _$UIFieldDef([void Function(UIFieldDefBuilder)? updates]) =>
      (UIFieldDefBuilder()..update(updates))._build();

  _$UIFieldDef._(
      {required this.name,
      required this.label,
      required this.type,
      this.requiredCommaOmitempty,
      this.defaultCommaOmitempty,
      this.optionsCommaOmitempty,
      this.placeholderCommaOmitempty,
      this.helpCommaOmitempty})
      : super._();
  @override
  UIFieldDef rebuild(void Function(UIFieldDefBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  UIFieldDefBuilder toBuilder() => UIFieldDefBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is UIFieldDef &&
        name == other.name &&
        label == other.label &&
        type == other.type &&
        requiredCommaOmitempty == other.requiredCommaOmitempty &&
        defaultCommaOmitempty == other.defaultCommaOmitempty &&
        optionsCommaOmitempty == other.optionsCommaOmitempty &&
        placeholderCommaOmitempty == other.placeholderCommaOmitempty &&
        helpCommaOmitempty == other.helpCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, label.hashCode);
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, requiredCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, defaultCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, optionsCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, placeholderCommaOmitempty.hashCode);
    _$hash = $jc(_$hash, helpCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'UIFieldDef')
          ..add('name', name)
          ..add('label', label)
          ..add('type', type)
          ..add('requiredCommaOmitempty', requiredCommaOmitempty)
          ..add('defaultCommaOmitempty', defaultCommaOmitempty)
          ..add('optionsCommaOmitempty', optionsCommaOmitempty)
          ..add('placeholderCommaOmitempty', placeholderCommaOmitempty)
          ..add('helpCommaOmitempty', helpCommaOmitempty))
        .toString();
  }
}

class UIFieldDefBuilder implements Builder<UIFieldDef, UIFieldDefBuilder> {
  _$UIFieldDef? _$v;

  String? _name;
  String? get name => _$this._name;
  set name(String? name) => _$this._name = name;

  String? _label;
  String? get label => _$this._label;
  set label(String? label) => _$this._label = label;

  String? _type;
  String? get type => _$this._type;
  set type(String? type) => _$this._type = type;

  bool? _requiredCommaOmitempty;
  bool? get requiredCommaOmitempty => _$this._requiredCommaOmitempty;
  set requiredCommaOmitempty(bool? requiredCommaOmitempty) =>
      _$this._requiredCommaOmitempty = requiredCommaOmitempty;

  JsonObject? _defaultCommaOmitempty;
  JsonObject? get defaultCommaOmitempty => _$this._defaultCommaOmitempty;
  set defaultCommaOmitempty(JsonObject? defaultCommaOmitempty) =>
      _$this._defaultCommaOmitempty = defaultCommaOmitempty;

  String? _optionsCommaOmitempty;
  String? get optionsCommaOmitempty => _$this._optionsCommaOmitempty;
  set optionsCommaOmitempty(String? optionsCommaOmitempty) =>
      _$this._optionsCommaOmitempty = optionsCommaOmitempty;

  String? _placeholderCommaOmitempty;
  String? get placeholderCommaOmitempty => _$this._placeholderCommaOmitempty;
  set placeholderCommaOmitempty(String? placeholderCommaOmitempty) =>
      _$this._placeholderCommaOmitempty = placeholderCommaOmitempty;

  String? _helpCommaOmitempty;
  String? get helpCommaOmitempty => _$this._helpCommaOmitempty;
  set helpCommaOmitempty(String? helpCommaOmitempty) =>
      _$this._helpCommaOmitempty = helpCommaOmitempty;

  UIFieldDefBuilder() {
    UIFieldDef._defaults(this);
  }

  UIFieldDefBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _name = $v.name;
      _label = $v.label;
      _type = $v.type;
      _requiredCommaOmitempty = $v.requiredCommaOmitempty;
      _defaultCommaOmitempty = $v.defaultCommaOmitempty;
      _optionsCommaOmitempty = $v.optionsCommaOmitempty;
      _placeholderCommaOmitempty = $v.placeholderCommaOmitempty;
      _helpCommaOmitempty = $v.helpCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(UIFieldDef other) {
    _$v = other as _$UIFieldDef;
  }

  @override
  void update(void Function(UIFieldDefBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  UIFieldDef build() => _build();

  _$UIFieldDef _build() {
    final _$result = _$v ??
        _$UIFieldDef._(
          name: BuiltValueNullFieldError.checkNotNull(
              name, r'UIFieldDef', 'name'),
          label: BuiltValueNullFieldError.checkNotNull(
              label, r'UIFieldDef', 'label'),
          type: BuiltValueNullFieldError.checkNotNull(
              type, r'UIFieldDef', 'type'),
          requiredCommaOmitempty: requiredCommaOmitempty,
          defaultCommaOmitempty: defaultCommaOmitempty,
          optionsCommaOmitempty: optionsCommaOmitempty,
          placeholderCommaOmitempty: placeholderCommaOmitempty,
          helpCommaOmitempty: helpCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
