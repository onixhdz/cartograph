// inference.h — public API for embedding inference.
// llama.cpp handles architecture dispatch internally.

#ifndef CARTOGRAPH_INFERENCE_H
#define CARTOGRAPH_INFERENCE_H

#include <stdint.h>

typedef struct cg_model cg_model;

// Load a GGUF model from path. Returns NULL on failure.
cg_model * cg_model_init(const char * path, int n_threads_batch);

// Query model properties.
int cg_model_dims(cg_model * m);
int cg_model_max_seq(cg_model * m);

// I/O buffer accessors. Write token IDs and attention mask,
// call cg_embed_tokens, then read output.
int32_t * cg_model_input_ids(cg_model * m);
int32_t * cg_model_attn_mask(cg_model * m);
float   * cg_model_output(cg_model * m);

// Tokenize text using the model's built-in tokenizer (read from GGUF).
// Writes token IDs into out_ids, returns the number of tokens produced.
// max_tokens is the capacity of out_ids. add_special=1 adds BOS/EOS.
int cg_tokenize(cg_model * m, const char * text, int32_t * out_ids, int max_tokens, int add_special);

// Run forward pass on seq_len tokens. Returns 0 on success.
int cg_embed_tokens(cg_model * m, int seq_len);

// Free model and all associated memory.
void cg_model_free(cg_model * m);

void * cg_malloc(unsigned long size);
void   cg_free(void * ptr);

#endif // CARTOGRAPH_INFERENCE_H
