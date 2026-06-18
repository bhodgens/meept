//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'trigger_request.g.dart';

/// TriggerRequest
///
/// Properties:
/// * [forceCommaOmitempty] 
@BuiltValue()
abstract class TriggerRequest implements Built<TriggerRequest, TriggerRequestBuilder> {
  @BuiltValueField(wireName: r'force,omitempty')
  bool? get forceCommaOmitempty;

  TriggerRequest._();

  factory TriggerRequest([void updates(TriggerRequestBuilder b)]) = _$TriggerRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(TriggerRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<TriggerRequest> get serializer => _$TriggerRequestSerializer();
}

class _$TriggerRequestSerializer implements PrimitiveSerializer<TriggerRequest> {
  @override
  final Iterable<Type> types = const [TriggerRequest, _$TriggerRequest];

  @override
  final String wireName = r'TriggerRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    TriggerRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    if (object.forceCommaOmitempty != null) {
      yield r'force,omitempty';
      yield serializers.serialize(
        object.forceCommaOmitempty,
        specifiedType: const FullType(bool),
      );
    }
  }

  @override
  Object serialize(
    Serializers serializers,
    TriggerRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required TriggerRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'force,omitempty':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(bool),
          ) as bool;
          result.forceCommaOmitempty = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  TriggerRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = TriggerRequestBuilder();
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

