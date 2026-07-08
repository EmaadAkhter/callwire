package dev.callwire.cli;

import java.io.*;
import java.nio.file.*;
import java.util.*;

/**
 * Callwire CLI: scan repo for workers, generate callwire.toml
 */
public class Main {

    public static void main(String[] args) throws Exception {
        if (args.length == 0 || "init".equals(args[0])) {
            generateCallwireToml();
        } else {
            System.err.println("Usage: callwire [init]");
            System.exit(1);
        }
    }

    private static void generateCallwireToml() throws IOException {
        Path root = Paths.get(".");
        StringBuilder toml = new StringBuilder();

        toml.append("[project]\n");
        toml.append("name = \"callwire-project\"\n");
        toml.append("version = \"1.0.0\"\n");
        toml.append("\n");

        // Scan for Go workers
        scanForWorkers(root, toml, "go", "Go", "cd go/callwire && go run server.go");
        // Scan for Python workers
        scanForWorkers(root, toml, "python", "Python", "cd python && python3 -m callwire.server");
        // Scan for Rust workers
        scanForWorkers(root, toml, "rust", "Rust", "cd rust && cargo run --quiet --release");
        // Scan for TypeScript workers
        scanForWorkers(root, toml, "ts", "TypeScript", "cd ts && npx tsx src/server.ts");
        // Scan for Java workers
        scanForWorkers(root, toml, "java", "Java", "cd java && mvn exec:java@server");

        Path configFile = Paths.get("callwire.toml");
        Files.write(configFile, toml.toString().getBytes());
        System.out.println("Generated callwire.toml");
    }

    private static void scanForWorkers(Path root, StringBuilder toml, String dir, String lang, String cmd)
            throws IOException {
        Path langDir = root.resolve(dir);
        if (Files.exists(langDir) && Files.isDirectory(langDir)) {
            String serviceName = dir + "-worker";
            toml.append("[services.").append(serviceName).append("]\n");
            toml.append("dev_cmd = \"").append(cmd).append("\"\n");
            toml.append("prod_cmd = \"").append(cmd).append("\"\n");
            toml.append("\n");
        }
    }
}
