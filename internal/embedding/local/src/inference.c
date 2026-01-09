// shim.c — wraps llama.cpp behind the cg_model API.

#include "inference.h"
#include "llama.h"

#include <stdlib.h>
#include <string.h>
#include <math.h>
#include <stdio.h>

// Cap context window to avoid excessive KV cache memory for large-context models.
#define CG_MAX_CTX 8192

struct cg_model {
    struct llama_model   * model;
    struct llama_context * ctx;
    int32_t * input_ids;
    int32_t * attn_mask;
    float   * output;
    int       n_embd;
    int       max_seq;
    int       is_decoder;
};

static void null_log(enum ggml_log_level level, const char * text, void * user_data) {
    (void)level; (void)text; (void)user_data;
}

__attribute__((export_name("cg_model_init")))
cg_model * cg_model_init(const char * path, int n_threads_batch) {
    llama_backend_init();
    llama_log_set(null_log, NULL);

    struct llama_model_params mp = llama_model_default_params();
    mp.use_mmap = true;

    struct llama_model * model = llama_model_load_from_file(path, mp);
    if (!model) return NULL;

    int n_embd = llama_model_n_embd(model);
    int n_ctx  = llama_model_n_ctx_train(model);
    if (n_ctx > CG_MAX_CTX) n_ctx = CG_MAX_CTX;

    int is_decoder = !llama_model_has_encoder(model) || llama_model_has_decoder(model);

    struct llama_context_params cp = llama_context_default_params();
    cp.n_ctx         = n_ctx;
    cp.n_batch       = n_ctx;
    cp.n_ubatch      = n_ctx;
    cp.embeddings    = true;
    // Let llama.cpp pick pooling from GGUF metadata; falls back to mean.
    cp.pooling_type  = LLAMA_POOLING_TYPE_UNSPECIFIED;
    cp.n_threads     = 1;
    cp.n_threads_batch = n_threads_batch > 0 ? n_threads_batch : 1;

    struct llama_context * ctx = llama_init_from_model(model, cp);
    if (!ctx) {
        llama_model_free(model);
        return NULL;
    }

    cg_model * m = (cg_model *)calloc(1, sizeof(cg_model));
    m->model      = model;
    m->ctx        = ctx;
    m->n_embd     = n_embd;
    m->max_seq    = n_ctx;
    m->is_decoder = is_decoder;
    m->input_ids  = (int32_t *)calloc(n_ctx, sizeof(int32_t));
    m->attn_mask  = (int32_t *)calloc(n_ctx, sizeof(int32_t));
    m->output     = (float *)calloc(n_embd, sizeof(float));

    return m;
}

__attribute__((export_name("cg_model_dims")))
int cg_model_dims(cg_model * m)       { return m ? m->n_embd : 0; }

__attribute__((export_name("cg_model_max_seq")))
int cg_model_max_seq(cg_model * m)    { return m ? m->max_seq : 0; }

__attribute__((export_name("cg_model_input_ids")))
int32_t * cg_model_input_ids(cg_model * m) { return m ? m->input_ids : NULL; }

__attribute__((export_name("cg_model_attn_mask")))
int32_t * cg_model_attn_mask(cg_model * m) { return m ? m->attn_mask : NULL; }

__attribute__((export_name("cg_model_output")))
float * cg_model_output(cg_model * m) { return m ? m->output : NULL; }

__attribute__((export_name("cg_tokenize")))
int cg_tokenize(cg_model * m, const char * text, int32_t * out_ids, int max_tokens, int add_special) {
    if (!m || !text || !out_ids || max_tokens <= 0) return -1;

    const struct llama_vocab * vocab = llama_model_get_vocab(m->model);
    int n = llama_tokenize(vocab, text, (int)strlen(text),
                           (llama_token *)out_ids, max_tokens,
                           add_special ? true : false,
                           true /* parse_special */);
    return n; // negative on overflow
}

__attribute__((export_name("cg_embed_tokens")))
int cg_embed_tokens(cg_model * m, int seq_len) {
    if (!m || seq_len <= 0 || seq_len > m->max_seq) return -1;

    llama_memory_clear(llama_get_memory(m->ctx), true);

    struct llama_batch batch = llama_batch_init(seq_len, 0, 1);
    batch.n_tokens = seq_len;
    for (int i = 0; i < seq_len; i++) {
        batch.token[i]    = m->input_ids[i];
        batch.pos[i]      = i;
        batch.n_seq_id[i] = 1;
        batch.seq_id[i][0] = 0;
        batch.logits[i]   = 0;
    }
    // Last-token logits needed for decoder models with last-token pooling.
    batch.logits[seq_len - 1] = 1;

    int rc;
    if (m->is_decoder) {
        rc = llama_decode(m->ctx, batch);
    } else {
        rc = llama_encode(m->ctx, batch);
    }
    llama_batch_free(batch);
    if (rc != 0) return rc;

    const float * embd = llama_get_embeddings_seq(m->ctx, 0);
    if (!embd) return -1;

    // L2 normalize
    float norm = 0.0f;
    for (int i = 0; i < m->n_embd; i++) {
        norm += embd[i] * embd[i];
    }
    norm = sqrtf(norm);
    if (norm > 0.0f) {
        for (int i = 0; i < m->n_embd; i++) {
            m->output[i] = embd[i] / norm;
        }
    } else {
        memcpy(m->output, embd, m->n_embd * sizeof(float));
    }

    return 0;
}

__attribute__((export_name("cg_model_free")))
void cg_model_free(cg_model * m) {
    if (!m) return;
    if (m->ctx)   llama_free(m->ctx);
    if (m->model) llama_model_free(m->model);
    free(m->input_ids);
    free(m->attn_mask);
    free(m->output);
    free(m);
}

__attribute__((export_name("cg_malloc")))
void * cg_malloc(unsigned long size) { return malloc(size); }

__attribute__((export_name("cg_free")))
void cg_free(void * ptr) { free(ptr); }
