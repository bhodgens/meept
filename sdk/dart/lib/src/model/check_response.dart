//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'check_response.g.dart';

/// CheckResponse
///
/// Properties:
/// * [allowed] 
/// * [reasonCommaOmitempty] 
@BuiltValue()
abstract class CheckResponse implements Built<CheckResponse, CheckResponseBuilder> {
  @BuiltValueField(wireName: r'allowed')
  bool get allowed;

  @BuiltValueField(wireName: r'reason,omitempty')
  String? get reasonCommaOmitempty;

  CheckResponse._();

  factory CheckResponse([void updates(CheckResponseBuilder b)]) = _$CheckResponse;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CheckResponseBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CheckResponse> get serializer => _$CheckResponseSerializer();
}

class _$CheckResponseSerializer implements PrimitiveSerializer<CheckResponse> {
  @override
  final Iterable<Type> types = const [CheckResponse, _$CheckResponse];

  @override
  final String wireName = r'CheckResponse';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CheckResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'allowed';
    yield serializers.serialize(
      object.allowed,
      specifiedType: const FullType(bool),
    );
    if (object.reasonCommaOmitempty != null) {
      yield r'reason,omitempty';
      yield serializers.serialize(
        object.reasonCommaOmitempty,
        specifiedType: const FullType(String),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    CheckResponse object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CheckResponseBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'allowed':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.allowed = valueDes;
          break;
        case r'reason,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.reasonCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CheckResponse deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CheckResponseBuilder();
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

