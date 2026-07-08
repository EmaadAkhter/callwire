package dev.callwire.core;

import org.msgpack.core.MessageBufferPacker;
import org.msgpack.core.MessagePack;
import org.msgpack.core.MessageUnpacker;
import org.msgpack.value.Value;

import java.io.IOException;
import java.util.*;

/**
 * Codec: msgpack encode/decode for wire messages.
 * Wire message is a msgpack map with string keys: id, type, func, args, stream, result, error_type, message.
 */
public class Codec {

    public static byte[] encodeRequest(long id, String func, List<Object> args) throws IOException {
        return encodeRequest(id, func, args, false);
    }

    public static byte[] encodeRequest(long id, String func, List<Object> args, boolean stream) throws IOException {
        MessageBufferPacker packer = MessagePack.newDefaultBufferPacker();
        packer.packMapHeader(stream ? 5 : 4);
        packer.packString("id");
        packer.packLong(id);
        packer.packString("type");
        packer.packString("request");
        packer.packString("func");
        packer.packString(func);
        packer.packString("args");
        packer.packArrayHeader(args.size());
        for (Object arg : args) {
            packValue(packer, arg);
        }
        if (stream) {
            packer.packString("stream");
            packer.packBoolean(true);
        }
        return packer.toByteArray();
    }

    public static byte[] encodeResponse(long id, Object result) throws IOException {
        MessageBufferPacker packer = MessagePack.newDefaultBufferPacker();
        packer.packMapHeader(3);
        packer.packString("id");
        packer.packLong(id);
        packer.packString("type");
        packer.packString("response");
        packer.packString("result");
        packValue(packer, result);
        return packer.toByteArray();
    }

    public static byte[] encodeError(long id, String errorType, String message) throws IOException {
        MessageBufferPacker packer = MessagePack.newDefaultBufferPacker();
        packer.packMapHeader(4);
        packer.packString("id");
        packer.packLong(id);
        packer.packString("type");
        packer.packString("error");
        packer.packString("error_type");
        packer.packString(errorType);
        packer.packString("message");
        packer.packString(message);
        return packer.toByteArray();
    }

    public static byte[] encodeStreamChunk(long id, Object result) throws IOException {
        MessageBufferPacker packer = MessagePack.newDefaultBufferPacker();
        packer.packMapHeader(3);
        packer.packString("id");
        packer.packLong(id);
        packer.packString("type");
        packer.packString("stream_chunk");
        packer.packString("result");
        packValue(packer, result);
        return packer.toByteArray();
    }

    public static byte[] encodeStreamEnd(long id) throws IOException {
        MessageBufferPacker packer = MessagePack.newDefaultBufferPacker();
        packer.packMapHeader(2);
        packer.packString("id");
        packer.packLong(id);
        packer.packString("type");
        packer.packString("stream_end");
        return packer.toByteArray();
    }

    public static byte[] encodeStreamClose(long id) throws IOException {
        MessageBufferPacker packer = MessagePack.newDefaultBufferPacker();
        packer.packMapHeader(2);
        packer.packString("id");
        packer.packLong(id);
        packer.packString("type");
        packer.packString("stream_close");
        return packer.toByteArray();
    }

    public static Map<String, Object> decode(byte[] payload) throws IOException {
        MessageUnpacker unpacker = MessagePack.newDefaultUnpacker(payload);
        Map<String, Object> map = new HashMap<>();
        int mapSize = unpacker.unpackMapHeader();
        for (int i = 0; i < mapSize; i++) {
            String key = unpacker.unpackString();
            Value value = unpacker.unpackValue();
            map.put(key, unpackValue(value));
        }
        return map;
    }

    private static void packValue(MessageBufferPacker packer, Object value) throws IOException {
        if (value == null) {
            packer.packNil();
        } else if (value instanceof Boolean) {
            packer.packBoolean((Boolean) value);
        } else if (value instanceof Integer) {
            packer.packInt((Integer) value);
        } else if (value instanceof Long) {
            packer.packLong((Long) value);
        } else if (value instanceof Float) {
            packer.packFloat((Float) value);
        } else if (value instanceof Double) {
            packer.packDouble((Double) value);
        } else if (value instanceof String) {
            packer.packString((String) value);
        } else if (value instanceof List) {
            List<?> list = (List<?>) value;
            packer.packArrayHeader(list.size());
            for (Object item : list) {
                packValue(packer, item);
            }
        } else if (value instanceof Map) {
            Map<?, ?> map = (Map<?, ?>) value;
            packer.packMapHeader(map.size());
            for (Map.Entry<?, ?> entry : map.entrySet()) {
                packValue(packer, entry.getKey());
                packValue(packer, entry.getValue());
            }
        } else {
            packer.packString(value.toString());
        }
    }

    private static Object unpackValue(Value value) {
        if (value.isNilValue()) {
            return null;
        } else if (value.isBooleanValue()) {
            return value.asBooleanValue().getBoolean();
        } else if (value.isIntegerValue()) {
            try {
                return value.asIntegerValue().toLong();
            } catch (Exception e) {
                return value.asIntegerValue().toInt();
            }
        } else if (value.isFloatValue()) {
            return value.asFloatValue().toDouble();
        } else if (value.isStringValue()) {
            return value.asStringValue().asString();
        } else if (value.isArrayValue()) {
            List<Object> list = new ArrayList<>();
            for (Value elem : value.asArrayValue()) {
                list.add(unpackValue(elem));
            }
            return list;
        } else if (value.isMapValue()) {
            Map<String, Object> map = new HashMap<>();
            for (Map.Entry<Value, Value> entry : value.asMapValue().entrySet()) {
                map.put(unpackValue(entry.getKey()).toString(), unpackValue(entry.getValue()));
            }
            return map;
        }
        return value.toString();
    }
}
