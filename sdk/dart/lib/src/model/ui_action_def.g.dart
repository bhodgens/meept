// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'ui_action_def.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$UIActionDef extends UIActionDef {
  @override
  final String id;
  @override
  final String label;
  @override
  final String type;
  @override
  final String? styleCommaOmitempty;

  factory _$UIActionDef([void Function(UIActionDefBuilder)? updates]) =>
      (UIActionDefBuilder()..update(updates))._build();

  _$UIActionDef._(
      {required this.id,
      required this.label,
      required this.type,
      this.styleCommaOmitempty})
      : super._();
  @override
  UIActionDef rebuild(void Function(UIActionDefBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  UIActionDefBuilder toBuilder() => UIActionDefBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is UIActionDef &&
        id == other.id &&
        label == other.label &&
        type == other.type &&
        styleCommaOmitempty == other.styleCommaOmitempty;
  }

  @override
  int get hashCode {
    var _$hash = 0;
    _$hash = $jc(_$hash, id.hashCode);
    _$hash = $jc(_$hash, label.hashCode);
    _$hash = $jc(_$hash, type.hashCode);
    _$hash = $jc(_$hash, styleCommaOmitempty.hashCode);
    _$hash = $jf(_$hash);
    return _$hash;
  }

  @override
  String toString() {
    return (newBuiltValueToStringHelper(r'UIActionDef')
          ..add('id', id)
          ..add('label', label)
          ..add('type', type)
          ..add('styleCommaOmitempty', styleCommaOmitempty))
        .toString();
  }
}

class UIActionDefBuilder implements Builder<UIActionDef, UIActionDefBuilder> {
  _$UIActionDef? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  String? _label;
  String? get label => _$this._label;
  set label(String? label) => _$this._label = label;

  String? _type;
  String? get type => _$this._type;
  set type(String? type) => _$this._type = type;

  String? _styleCommaOmitempty;
  String? get styleCommaOmitempty => _$this._styleCommaOmitempty;
  set styleCommaOmitempty(String? styleCommaOmitempty) =>
      _$this._styleCommaOmitempty = styleCommaOmitempty;

  UIActionDefBuilder() {
    UIActionDef._defaults(this);
  }

  UIActionDefBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _label = $v.label;
      _type = $v.type;
      _styleCommaOmitempty = $v.styleCommaOmitempty;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(UIActionDef other) {
    _$v = other as _$UIActionDef;
  }

  @override
  void update(void Function(UIActionDefBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  UIActionDef build() => _build();

  _$UIActionDef _build() {
    final _$result = _$v ??
        _$UIActionDef._(
          id: BuiltValueNullFieldError.checkNotNull(id, r'UIActionDef', 'id'),
          label: BuiltValueNullFieldError.checkNotNull(
              label, r'UIActionDef', 'label'),
          type: BuiltValueNullFieldError.checkNotNull(
              type, r'UIActionDef', 'type'),
          styleCommaOmitempty: styleCommaOmitempty,
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
