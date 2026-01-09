const std = @import("std");

pub fn build(b: *std.Build) void {
    const optimize = b.standardOptimizeOption(.{});

    const llama_dep = b.dependency("llama_cpp", .{});

    // ── C/C++ flags ──────────────────────────────────────────────────
    const common_c_flags: []const []const u8 = &.{
        "-DNDEBUG",
        "-D_GNU_SOURCE",
        "-DGGML_VERSION=\"b8658\"",
        "-DGGML_COMMIT=\"b8658\"",
        "-DGGML_USE_CPU",
    };

    const common_cpp_flags: []const []const u8 = &.{
        "-DNDEBUG",
        "-D_GNU_SOURCE",
        "-DGGML_VERSION=\"b8658\"",
        "-DGGML_COMMIT=\"b8658\"",
        "-DGGML_USE_CPU",
        "-std=c++17",
    };

    // ── ggml core C sources ──────────────────────────────────────────
    const ggml_c_sources: []const []const u8 = &.{
        "ggml/src/ggml.c",
        "ggml/src/ggml-alloc.c",
        "ggml/src/ggml-quants.c",
        "ggml/src/ggml-cpu/ggml-cpu.c",
        "ggml/src/ggml-cpu/quants.c",
    };

    // ── ggml C++ sources ─────────────────────────────────────────────
    const ggml_cpp_sources: []const []const u8 = &.{
        "ggml/src/ggml-backend.cpp",
        "ggml/src/ggml-backend-reg.cpp",
        "ggml/src/ggml-backend-dl.cpp",
        "ggml/src/ggml.cpp",
        "ggml/src/gguf.cpp",
        "ggml/src/ggml-threading.cpp",
        "ggml/src/ggml-opt.cpp",
        "ggml/src/ggml-cpu/ggml-cpu.cpp",
        "ggml/src/ggml-cpu/ops.cpp",
        "ggml/src/ggml-cpu/binary-ops.cpp",
        "ggml/src/ggml-cpu/unary-ops.cpp",
        "ggml/src/ggml-cpu/vec.cpp",
        "ggml/src/ggml-cpu/traits.cpp",
        "ggml/src/ggml-cpu/repack.cpp",
        "ggml/src/ggml-cpu/hbm.cpp",
    };

    // ── Architecture-specific CPU sources ────────────────────────────
    const arch_arm_c_sources: []const []const u8 = &.{
        "ggml/src/ggml-cpu/arch/arm/quants.c",
    };
    const arch_arm_cpp_sources: []const []const u8 = &.{
        "ggml/src/ggml-cpu/arch/arm/cpu-feats.cpp",
        "ggml/src/ggml-cpu/arch/arm/repack.cpp",
    };
    const arch_x86_c_sources: []const []const u8 = &.{
        "ggml/src/ggml-cpu/arch/x86/quants.c",
    };
    const arch_x86_cpp_sources: []const []const u8 = &.{
        "ggml/src/ggml-cpu/arch/x86/cpu-feats.cpp",
        "ggml/src/ggml-cpu/arch/x86/repack.cpp",
    };

    // ── llama.cpp core sources ───────────────────────────────────────
    const llama_cpp_sources: []const []const u8 = &.{
        "src/llama.cpp",
        "src/llama-model.cpp",
        "src/llama-model-loader.cpp",
        "src/llama-model-saver.cpp",
        "src/llama-arch.cpp",
        "src/llama-hparams.cpp",
        "src/llama-vocab.cpp",
        "src/llama-context.cpp",
        "src/llama-kv-cache.cpp",
        "src/llama-kv-cache-iswa.cpp",
        "src/llama-batch.cpp",
        "src/llama-mmap.cpp",
        "src/llama-impl.cpp",
        "src/llama-graph.cpp",
        "src/llama-memory.cpp",
        "src/llama-memory-recurrent.cpp",
        "src/llama-memory-hybrid.cpp",
        "src/llama-memory-hybrid-iswa.cpp",
        "src/llama-cparams.cpp",
        "src/llama-io.cpp",
        "src/llama-adapter.cpp",
        "src/llama-chat.cpp",
        "src/llama-grammar.cpp",
        "src/llama-sampler.cpp",
        "src/llama-quant.cpp",
        "src/unicode.cpp",
        "src/unicode-data.cpp",
    };

    // ── llama.cpp model architecture sources ─────────────────────────
    const llama_model_sources: []const []const u8 = &.{
        "src/models/bert.cpp",
        "src/models/bloom.cpp",
        "src/models/gpt2.cpp",
        "src/models/gptneox.cpp",
        "src/models/llama.cpp",
        "src/models/llama-iswa.cpp",
        "src/models/falcon.cpp",
        "src/models/gemma.cpp",
        "src/models/gemma-embedding.cpp",
        "src/models/gemma2-iswa.cpp",
        "src/models/gemma3.cpp",
        "src/models/modern-bert.cpp",
        "src/models/neo-bert.cpp",
        "src/models/eurobert.cpp",
        "src/models/starcoder.cpp",
        "src/models/starcoder2.cpp",
        "src/models/refact.cpp",
        "src/models/phi2.cpp",
        "src/models/phi3.cpp",
        "src/models/qwen.cpp",
        "src/models/qwen2.cpp",
        "src/models/qwen2moe.cpp",
        "src/models/qwen2vl.cpp",
        "src/models/qwen3.cpp",
        "src/models/qwen3moe.cpp",
        "src/models/qwen35.cpp",
        "src/models/qwen35moe.cpp",
        "src/models/qwen3next.cpp",
        "src/models/qwen3vl.cpp",
        "src/models/qwen3vl-moe.cpp",
        "src/models/olmo.cpp",
        "src/models/olmo2.cpp",
        "src/models/olmoe.cpp",
        "src/models/granite.cpp",
        "src/models/granite-hybrid.cpp",
        "src/models/mpt.cpp",
        "src/models/stablelm.cpp",
        "src/models/internlm2.cpp",
        "src/models/chameleon.cpp",
        "src/models/chatglm.cpp",
        "src/models/codeshell.cpp",
        "src/models/orion.cpp",
        "src/models/openelm.cpp",
        "src/models/arctic.cpp",
        "src/models/deepseek.cpp",
        "src/models/deepseek2.cpp",
        "src/models/baichuan.cpp",
        "src/models/plamo.cpp",
        "src/models/plamo2.cpp",
        "src/models/plamo3.cpp",
        "src/models/command-r.cpp",
        "src/models/cohere2-iswa.cpp",
        "src/models/dbrx.cpp",
        "src/models/exaone.cpp",
        "src/models/exaone-moe.cpp",
        "src/models/exaone4.cpp",
        "src/models/jamba.cpp",
        "src/models/mamba.cpp",
        "src/models/mamba-base.cpp",
        "src/models/nemotron.cpp",
        "src/models/nemotron-h.cpp",
        "src/models/t5-enc.cpp",
        "src/models/t5-dec.cpp",
        "src/models/jais.cpp",
        "src/models/jais2.cpp",
        "src/models/minicpm3.cpp",
        "src/models/deci.cpp",
        "src/models/grok.cpp",
        "src/models/bitnet.cpp",
        "src/models/wavtokenizer-dec.cpp",
        "src/models/rwkv6.cpp",
        "src/models/rwkv6-base.cpp",
        "src/models/rwkv6qwen2.cpp",
        "src/models/rwkv7.cpp",
        "src/models/rwkv7-base.cpp",
        "src/models/arwkv7.cpp",
        "src/models/delta-net-base.cpp",
        "src/models/falcon-h1.cpp",
        "src/models/cogvlm.cpp",
        "src/models/xverse.cpp",
        "src/models/afmoe.cpp",
        "src/models/apertus.cpp",
        "src/models/arcee.cpp",
        "src/models/bailingmoe.cpp",
        "src/models/bailingmoe2.cpp",
        "src/models/dots1.cpp",
        "src/models/dream.cpp",
        "src/models/ernie4-5.cpp",
        "src/models/ernie4-5-moe.cpp",
        "src/models/gemma3n-iswa.cpp",
        "src/models/gemma4-iswa.cpp",
        "src/models/glm4.cpp",
        "src/models/glm4-moe.cpp",
        "src/models/grovemoe.cpp",
        "src/models/hunyuan-dense.cpp",
        "src/models/hunyuan-moe.cpp",
        "src/models/kimi-linear.cpp",
        "src/models/lfm2.cpp",
        "src/models/llada.cpp",
        "src/models/llada-moe.cpp",
        "src/models/maincoder.cpp",
        "src/models/mimo2-iswa.cpp",
        "src/models/minimax-m2.cpp",
        "src/models/mistral3.cpp",
        "src/models/openai-moe-iswa.cpp",
        "src/models/paddleocr.cpp",
        "src/models/pangu-embedded.cpp",
        "src/models/plm.cpp",
        "src/models/rnd1.cpp",
        "src/models/seed-oss.cpp",
        "src/models/smallthinker.cpp",
        "src/models/smollm3.cpp",
        "src/models/step35-iswa.cpp",
    };

    // ── Native target (libinference.a) ───────────────────────────────
    const native_step = b.step("native", "Build libinference.a (native static library, llama.cpp backend)");
    {
        const native_mod = b.createModule(.{
            .target = b.standardTargetOptions(.{}),
            .optimize = optimize,
        });

        const lib = b.addLibrary(.{
            .name = "inference",
            .linkage = .static,
            .root_module = native_mod,
        });

        // Include paths
        native_mod.addIncludePath(llama_dep.path("ggml/include"));
        native_mod.addIncludePath(llama_dep.path("ggml/src"));
        native_mod.addIncludePath(llama_dep.path("ggml/src/ggml-cpu"));
        native_mod.addIncludePath(llama_dep.path("include"));
        native_mod.addIncludePath(llama_dep.path("src"));
        native_mod.addIncludePath(b.path("src"));

        // ggml core C sources
        native_mod.addCSourceFiles(.{
            .root = llama_dep.path(""),
            .files = ggml_c_sources,
            .flags = common_c_flags,
        });

        // ggml C++ sources
        native_mod.addCSourceFiles(.{
            .root = llama_dep.path(""),
            .files = ggml_cpp_sources,
            .flags = common_cpp_flags,
        });

        // Architecture-specific CPU sources
        const native_target = native_mod.resolved_target.?.result;
        if (native_target.cpu.arch == .aarch64) {
            native_mod.addCSourceFiles(.{
                .root = llama_dep.path(""),
                .files = arch_arm_c_sources,
                .flags = common_c_flags,
            });
            native_mod.addCSourceFiles(.{
                .root = llama_dep.path(""),
                .files = arch_arm_cpp_sources,
                .flags = common_cpp_flags,
            });
        } else if (native_target.cpu.arch == .x86_64 or native_target.cpu.arch == .x86) {
            native_mod.addCSourceFiles(.{
                .root = llama_dep.path(""),
                .files = arch_x86_c_sources,
                .flags = common_c_flags,
            });
            native_mod.addCSourceFiles(.{
                .root = llama_dep.path(""),
                .files = arch_x86_cpp_sources,
                .flags = common_cpp_flags,
            });
        }

        // llama.cpp core sources
        native_mod.addCSourceFiles(.{
            .root = llama_dep.path(""),
            .files = llama_cpp_sources,
            .flags = common_cpp_flags,
        });

        // llama.cpp model architecture sources
        native_mod.addCSourceFiles(.{
            .root = llama_dep.path(""),
            .files = llama_model_sources,
            .flags = common_cpp_flags,
        });

        // Our inference shim (single C file)
        native_mod.addCSourceFiles(.{
            .files = &.{"src/inference.c"},
            .flags = common_c_flags,
        });

        // Link libc and libstdc++
        native_mod.link_libc = true;
        native_mod.link_libcpp = true;

        const install_lib = b.addInstallArtifact(lib, .{});

        // Install the header for CGO to find
        const install_header = b.addInstallFileWithDir(
            b.path("src/inference.h"),
            .header,
            "inference.h",
        );

        native_step.dependOn(&install_lib.step);
        native_step.dependOn(&install_header.step);

        // ── macOS TBD stubs ──────────────────────────────────────────
        if (native_target.os.tag == .macos) {
            const stubs = .{
                .{ "lib/libresolv.tbd", macos_tbd_libresolv },
                .{ "lib/CoreFoundation.framework/CoreFoundation.tbd", macos_tbd_cf },
                .{ "lib/Security.framework/Security.tbd", macos_tbd_sec },
            };
            inline for (stubs) |entry| {
                const wf = b.addWriteFiles();
                const path = wf.add(entry[0], entry[1]);
                const install_stub = b.addInstallFile(path, entry[0]);
                native_step.dependOn(&install_stub.step);
            }
        }
    }
}

// Minimal TBD stubs for macOS cross-compilation.
const macos_tbd_libresolv =
    \\--- !tapi-tbd
    \\tbd-version:     4
    \\targets:         [ x86_64-macos, arm64-macos ]
    \\install-name:    '/usr/lib/libresolv.9.dylib'
    \\exports: []
    \\...
    \\
;

const macos_tbd_cf =
    \\--- !tapi-tbd
    \\tbd-version:     4
    \\targets:         [ x86_64-macos, arm64-macos ]
    \\install-name:    '/System/Library/Frameworks/CoreFoundation.framework/Versions/A/CoreFoundation'
    \\exports:
    \\  - targets: [ x86_64-macos, arm64-macos ]
    \\    symbols: [ _CFArrayAppendValue, _CFArrayCreateMutable, _CFArrayGetCount,
    \\               _CFArrayGetValueAtIndex, _CFDataCreate, _CFDataGetBytePtr,
    \\               _CFDataGetLength, _CFDateCreate, _CFErrorCopyDescription,
    \\               _CFErrorGetCode, _CFRelease, _CFStringCreateExternalRepresentation,
    \\               _CFStringCreateWithBytes, _kCFAllocatorDefault,
    \\               _kCFTypeDictionaryKeyCallBacks, _kCFTypeDictionaryValueCallBacks ]
    \\...
    \\
;

const macos_tbd_sec =
    \\--- !tapi-tbd
    \\tbd-version:     4
    \\targets:         [ x86_64-macos, arm64-macos ]
    \\install-name:    '/System/Library/Frameworks/Security.framework/Versions/A/Security'
    \\exports:
    \\  - targets: [ x86_64-macos, arm64-macos ]
    \\    symbols: [ _SecCertificateCopyData, _SecCertificateCreateWithData,
    \\               _SecPolicyCreateSSL, _SecTrustCopyCertificateChain,
    \\               _SecTrustCreateWithCertificates, _SecTrustEvaluateWithError,
    \\               _SecTrustSetVerifyDate ]
    \\...
    \\
;
