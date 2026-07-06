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
  final bool? required_;
  @override
  final JsonObject? default_;
  @override
  final String? options;
  @override
  final String? placeholder;
  @override
  final String? help;

  factory _$UIFieldDef([void Function(UIFieldDefBuilder)? updates]) =>
      (UIFieldDefBuilder()..update(updates))._build();

  _$UIFieldDef._(
      {required this.name,
      required this.label,
      required this.type,
      this.required_,
      this.default_,
      this.options,
      this.placeholder,
      this.help})
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
        required_ == other.required_ &&
        default_ == other.default_ &&
        options == other.options &&
        placeholder == other.placeholder &&
        help == other.help;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, name.hashCode);
    _$hash = $jc(_$hash, label.hashCode);
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, required_.hashCode);
    _$hash = $jc(_$hash, default_.hashCode);
    _$hash = $jc(_$hash, options.hashCode);
    _$hash = $jc(_$hash, placeholder.hashCode);
    _$hash = $jc(_$hash, help.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'UIFieldDef')
          ..add('name', name)
          ..add('label', label)
          ..add('type', type)
          ..add('required_', required_)
          ..add('default_', default_)
          ..add('options', options)
          ..add('placeholder', placeholder)
          ..add('help', help))
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

  bool? _required_;
  bool? get required_ => _$this._required_;
  set required_(bool? required_) =>
      _$this._required_ = required_;

  JsonObject? _default_;
  JsonObject? get default_ => _$this._default_;
  set default_(JsonObject? default_) =>
      _$this._default_ = default_;

  String? _options;
  String? get options => _$this._options;
  set options(String? options) =>
      _$this._options = options;

  String? _placeholder;
  String? get placeholder => _$this._placeholder;
  set placeholder(String? placeholder) =>
      _$this._placeholder = placeholder;

  String? _help;
  String? get help => _$this._help;
  set help(String? help) =>
      _$this._help = help;

  UIFieldDefBuilder() {
    UIFieldDef._defaults(this);
  }

  UIFieldDefBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _name = $v.name;
      _label = $v.label;
      _type = $v.type;
      _required_ = $v.required_;
      _default_ = $v.default_;
      _options = $v.options;
      _placeholder = $v.placeholder;
      _help = $v.help;
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
          required_: required_,
          default_: default_,
          options: options,
          placeholder: placeholder,
          help: help,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
