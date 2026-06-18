//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'cancel_request.g.dart';

/// CancelRequest
///
/// Properties:
/// * [cycleId] 
@BuiltValue()
abstract class CancelRequest implements Built<CancelRequest, CancelRequestBuilder> {
  @BuiltValueField(wireName: r'cycle_id')
  String get cycleId;

  CancelRequest._();

  factory CancelRequest([void updates(CancelRequestBuilder b)]) = _$CancelRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(CancelRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<CancelRequest> get serializer => _$CancelRequestSerializer();
}

class _$CancelRequestSerializer implements PrimitiveSerializer<CancelRequest> {
  @override
  final Iterable<Type> types = const [CancelRequest, _$CancelRequest];

  @override
  final String wireName = r'CancelRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    CancelRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'cycle_id';
    yield serializers.serialize(
      object.cycleId,
      specifiedType: const FullType(String),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    CancelRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required CancelRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'cycle_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.cycleId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  CancelRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = CancelRequestBuilder();
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

