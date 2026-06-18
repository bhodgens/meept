//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'list_options.g.dart';

/// ListOptions
///
/// Properties:
/// * [limitCommaOmitempty] 
/// * [offsetCommaOmitempty] 
/// * [filterCommaOmitempty] 
@BuiltValue()
abstract class ListOptions implements Built<ListOptions, ListOptionsBuilder> {
  @BuiltValueField(wireName: r'limit,omitempty')
  int? get limitCommaOmitempty;

  @BuiltValueField(wireName: r'offset,omitempty')
  int? get offsetCommaOmitempty;

  @BuiltValueField(wireName: r'filter,omitempty')
  String? get filterCommaOmitempty;

  ListOptions._();

  factory ListOptions([void updates(ListOptionsBuilder b)]) = _$ListOptions;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(ListOptionsBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<ListOptions> get serializer => _$ListOptionsSerializer();
}

class _$ListOptionsSerializer implements PrimitiveSerializer<ListOptions> {
  @override
  final Iterable<Type> types = const [ListOptions, _$ListOptions];

  @override
  final String wireName = r'ListOptions';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    ListOptions object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.limitCommaOmitempty != null) {
      yield r'limit,omitempty';
      yield serializers.serialize(
        object.limitCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
    if (object.offsetCommaOmitempty != null) {
      yield r'offset,omitempty';
      yield serializers.serialize(
        object.offsetCommaOmitempty,
        specifiedType: const FullType(int),
      );
    }
    if (object.filterCommaOmitempty != null) {
      yield r'filter,omitempty';
      yield serializers.serialize(
        object.filterCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    ListOptions object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required ListOptionsBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'limit,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.limitCommaOmitempty = valueDes;
          break;
        case r'offset,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.offsetCommaOmitempty = valueDes;
          break;
        case r'filter,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.filterCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  ListOptions deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = ListOptionsBuilder();
    final serializedList = (serialized as Iterable<Object?>).toList();
    final unhandled = <Object?>[];
    _deserializeProperties(
      serializers,
      serialized,
      specifiedType: specifiedType,
      serializedList: serializedList,
      unhandled: unhandled,
      result: result,
    );
    return result.build();
  }
}

