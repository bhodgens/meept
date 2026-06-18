//
// AUTO-GENERATED FILE, DO NOT MODIFY!
//

// ignore_for_file: unused_element
import 'package:built_value/built_value.dart';
import 'package:built_value/serializer.dart';

part 'branch_session_request.g.dart';

/// BranchSessionRequest
///
/// Properties:
/// * [id] 
/// * [targetMessageId] 
@BuiltValue()
abstract class BranchSessionRequest implements Built<BranchSessionRequest, BranchSessionRequestBuilder> {
  @BuiltValueField(wireName: r'id')
  String get id;

  @BuiltValueField(wireName: r'target_message_id')
  int get targetMessageId;

  BranchSessionRequest._();

  factory BranchSessionRequest([void updates(BranchSessionRequestBuilder b)]) = _$BranchSessionRequest;

  @BuiltValueHook(initializeBuilder: true)
  static void _defaults(BranchSessionRequestBuilder b) => b;

  @BuiltValueSerializer(custom: true)
  static Serializer<BranchSessionRequest> get serializer => _$BranchSessionRequestSerializer();
}

class _$BranchSessionRequestSerializer implements PrimitiveSerializer<BranchSessionRequest> {
  @override
  final Iterable<Type> types = const [BranchSessionRequest, _$BranchSessionRequest];

  @override
  final String wireName = r'BranchSessionRequest';

  Iterable<Object?> _serializeProperties(
    Serializers serializers,
    BranchSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) sync* {
    yield r'id';
    yield serializers.serialize(
      object.id,
      specifiedType: const FullType(String),
    );
    yield r'target_message_id';
    yield serializers.serialize(
      object.targetMessageId,
      specifiedType: const FullType(int),
    );
  }

  @override
  Object serialize(
    Serializers serializers,
    BranchSessionRequest object, {
    FullType specifiedType = FullType.unspecified,
  }) {
    return _serializeProperties(serializers, object, specifiedType: specifiedType).toList();
  }

  void _deserializeProperties(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
    required List<Object?> serializedList,
    required BranchSessionRequestBuilder result,
    required List<Object?> unhandled,
  }) {
    for (var i = 0; i < serializedList.length; i += 2) {
      final key = serializedList[i] as String;
      final value = serializedList[i + 1];
      switch (key) {
        case r'id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(String),
          ) as String;
          result.id = valueDes;
          break;
        case r'target_message_id':
          final valueDes = serializers.deserialize(
            value,
            specifiedType: const FullType(int),
          ) as int;
          result.targetMessageId = valueDes;
          break;
        default:
          unhandled.add(key);
          unhandled.add(value);
          break;
      }
    }
  }

  @override
  BranchSessionRequest deserialize(
    Serializers serializers,
    Object serialized, {
    FullType specifiedType = FullType.unspecified,
  }) {
    final result = BranchSessionRequestBuilder();
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

