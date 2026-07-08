import dev.callwire.core.Client;
import java.util.Arrays;

/**
 * Java client calling Go server.
 * Assumes Go server running on localhost:9090 with "add" function exported.
 *
 * Run Go server first:
 *   cd go/callwire && go run examples/server.go
 *
 * Then run this client:
 *   cd java && mvn exec:java -Dexec.mainClass=java_client
 */
public class java_client {
    public static void main(String[] args) throws Exception {
        Client client = new Client();
        client.connect("localhost", 9090);

        // Call add(10, 20)
        Object result = client.call("add", Arrays.asList(10L, 20L));
        System.out.println("add(10, 20) = " + result);

        // Call greet("World")
        Object greeting = client.call("greet", Arrays.asList("World"));
        System.out.println("greet(\"World\") = " + greeting);

        client.close();
    }
}
