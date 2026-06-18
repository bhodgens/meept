// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'get_tree_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$GetTreeRequest extends GetTreeRequest {
  @override
  final String id;

  factory _$GetTreeRequest([void Function(GetTreeRequestBuilder)? updates]) =>
      (GetTreeRequestBuilder()..update(updates))._build();

  _$GetTreeRequest._({required this.id}) : super._();
  @override
  GetTreeRequest rebuild(void Function(GetTreeRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  GetTreeRequestBuilder toBuilder() => GetTreeRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is GetTreeRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'GetTreeRequest')..add('id', id))
        .toString();
  }
}

class GetTreeRequestBuilder
    implements Builder<GetTreeRequest, GetTreeRequestBuilder> {
  _$GetTreeRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  GetTreeRequestBuilder() {
    GetTreeRequest._defaults(this);
  }

  GetTreeRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(GetTreeRequest other) {
    _$v = other as _$GetTreeRequest;
  }

  @override
  void update(void Function(GetTreeRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  GetTreeRequest build() => _build();

  _$GetTreeRequest _build() {
    final _$result = _$v ??
        _$GetTreeRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'GetTreeRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
