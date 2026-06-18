// GENERATED CODE - DO NOT MODIFY BY HAND

part of 'list_branches_request.dart';

// **************************************************************************
// BuiltValueGenerator
// **************************************************************************

class _$ListBranchesRequest extends ListBranchesRequest {
  @override
  final String id;

  factory _$ListBranchesRequest(
          [void Function(ListBranchesRequestBuilder)? updates]) =>
      (ListBranchesRequestBuilder()..update(updates))._build();

  _$ListBranchesRequest._({required this.id}) : super._();
  @override
  ListBranchesRequest rebuild(
          void Function(ListBranchesRequestBuilder) updates) =>
      (toBuilder()..update(updates)).build();

  @override
  ListBranchesRequestBuilder toBuilder() =>
      ListBranchesRequestBuilder()..replace(this);

  @override
  bool operator ==(Object other) {
    if (identical(other, this)) return true;
    return other is ListBranchesRequest && id == other.id;
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
    return (newBuiltValueToStringHelper(r'ListBranchesRequest')..add('id', id))
        .toString();
  }
}

class ListBranchesRequestBuilder
    implements Builder<ListBranchesRequest, ListBranchesRequestBuilder> {
  _$ListBranchesRequest? _$v;

  String? _id;
  String? get id => _$this._id;
  set id(String? id) => _$this._id = id;

  ListBranchesRequestBuilder() {
    ListBranchesRequest._defaults(this);
  }

  ListBranchesRequestBuilder get _$this {
    final $v = _$v;
    if ($v != null) {
      _id = $v.id;
      _$v = null;
    }
    return this;
  }

  @override
  void replace(ListBranchesRequest other) {
    _$v = other as _$ListBranchesRequest;
  }

  @override
  void update(void Function(ListBranchesRequestBuilder)? updates) {
    if (updates != null) updates(this);
  }

  @override
  ListBranchesRequest build() => _build();

  _$ListBranchesRequest _build() {
    final _$result = _$v ??
        _$ListBranchesRequest._(
          id: BuiltValueNullFieldError.checkNotNull(
              id, r'ListBranchesRequest', 'id'),
        );
    replace(_$result);
    return _$result;
  }
}

// ignore_for_file: deprecated_member_use_from_same_package,type=lint
