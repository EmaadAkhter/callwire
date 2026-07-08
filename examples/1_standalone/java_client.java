import dev.callwire.core.Client;
import java.util.Arrays;

/**
 * Java client calling local server.
 * Assumes server running on localhost:9090 with "add", "greet", "count_to" functions exported.
 *
 * Run server first:
 *   cd java && mvn exec:java -Dexec.mainClass=java_server
 *
 * Then run this client:
 *   cd java && mvn exec:java -Dexec.mainClass=java_client
 */
public class java_client {
    public static void main(String[] args) throws Exception {
        Client client = new Client();
        client.connect("localhost", 9090);

        // Unary: Call add(10, 20)
        Object result = client.call("add", Arrays.asList(10L, 20L));
        System.out.println("add(10, 20) = " + result);

        // Unary: Call greet("World")
        Object greeting = client.call("greet", Arrays.asList("World"));
        System.out.println("greet(\"World\") = " + greeting);

        // Server-streaming: Call count_to(5)
        System.out.println("count_to(5) stream:");
        for (Object chunk : client.callStream("count_to", Arrays.asList(5L))) {
            System.out.println("  " + chunk);
        }

        client.close();
    }
}
