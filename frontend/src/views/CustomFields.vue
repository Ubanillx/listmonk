<template>
  <section class="custom-fields">
    <div class="custom-fields__content">
      <header class="columns page-header is-vcentered">
        <div class="column">
          <h1 class="title is-4 mb-2">{{ $t('customFields.title') }}</h1>
          <p class="subtitle is-6 mb-0">{{ $t('customFields.help') }}</p>
        </div>
        <div v-if="isAdmin" class="column is-narrow">
          <b-button type="is-primary" icon-left="plus" :disabled="locked" @click="openForm()">
            {{ $t('customFields.add') }}
          </b-button>
        </div>
      </header>

      <b-notification v-if="locked" type="is-warning" :closable="false" class="custom-fields__notice">
        <b-icon icon="information-outline" size="is-small" />
        {{ $t('customFields.locked') }}
      </b-notification>

      <section class="box custom-fields__table-card">
        <b-loading :active="loading.customFields" :is-full-page="false" />
        <b-table :data="fields" striped hoverable narrowed class="custom-fields__table">
          <b-table-column field="label" :label="$t('customFields.label')" width="24%" v-slot="props">
            <div class="field-label">
              <span>{{ props.row.label }}</span>
              <b-tag v-if="props.row.system" size="is-small" type="is-light">{{ $t('customFields.system') }}</b-tag>
            </div>
          </b-table-column>
          <b-table-column field="key" :label="$t('customFields.key')" width="18%" v-slot="props">
            <code>{{ props.row.key }}</code>
          </b-table-column>
          <b-table-column field="type" :label="$t('customFields.type')" width="14%" v-slot="props">
            <b-tag type="is-light">{{ fieldTypeLabel(props.row.type) }}</b-tag>
          </b-table-column>
          <b-table-column :label="$t('customFields.placeholder')" v-slot="props">
            <code class="placeholder-code">{{ props.row.placeholder }}</code>
          </b-table-column>
          <b-table-column v-if="isAdmin" :label="$t('customFields.actions')" width="120" centered v-slot="props">
            <div v-if="!props.row.system" class="field-actions">
              <b-tooltip :label="$t('globals.buttons.edit')" type="is-dark">
                <b-button size="is-small" icon-left="pencil" :disabled="locked" @click="openForm(props.row)" />
              </b-tooltip>
              <b-tooltip :label="$t('globals.buttons.delete')" type="is-dark">
                <b-button size="is-small" type="is-danger" icon-left="delete" :disabled="locked" @click="remove(props.row)" />
              </b-tooltip>
            </div>
            <span v-else class="has-text-grey-light">—</span>
          </b-table-column>
          <template #empty v-if="!loading.customFields">
            <div class="has-text-centered has-text-grey py-6">{{ $t('globals.messages.emptyState') }}</div>
          </template>
        </b-table>
      </section>
    </div>

    <b-modal :active.sync="modal" has-modal-card trap-focus>
      <form class="modal-card" @submit.prevent="save">
        <header class="modal-card-head"><p class="modal-card-title">{{ editing ? $t('customFields.edit') : $t('customFields.add') }}</p></header>
        <section class="modal-card-body">
          <b-field :label="$t('customFields.key')" :message="$t('customFields.keyHelp')"><b-input v-model="form.key" pattern="[a-z][a-z0-9_]{0,63}" required :disabled="editing" /></b-field>
          <b-field :label="$t('customFields.label')"><b-input v-model="form.label" required /></b-field>
          <b-field :label="$t('customFields.type')"><b-select v-model="form.type" expanded><option v-for="t in types" :key="t" :value="t">{{ fieldTypeLabel(t) }}</option></b-select></b-field>
          <b-field v-if="form.type === 'select' || form.type === 'multi_select'"
            :label="$t('customFields.options')">
            <b-input v-model="form.optionsText" type="textarea" :placeholder="$t('customFields.optionsHelp')" />
          </b-field>
          <b-field :label="$t('customFields.description')"><b-input v-model="form.description" /></b-field>
          <b-checkbox v-model="form.required">{{ $t('customFields.required') }}</b-checkbox>
          <b-checkbox v-model="form.active" class="ml-4">{{ $t('customFields.active') }}</b-checkbox>
        </section>
        <footer class="modal-card-foot">
          <b-button @click="modal = false">{{ $t('globals.buttons.cancel') }}</b-button>
          <b-button native-type="submit" type="is-primary">{{ $t('globals.buttons.save') }}</b-button>
        </footer>
      </form>
    </b-modal>
  </section>
</template>
<script>
import Vue from 'vue';
import { mapState } from 'vuex';

export default Vue.extend({
  data() {
    return {
      fields: [], modal: false, editing: false, types: ['text', 'textarea', 'number', 'url', 'date', 'select', 'multi_select', 'checkbox'], form: {},
    };
  },
  computed: {
    ...mapState(['loading', 'profile']),
    isAdmin() { return !!(this.profile && this.profile.userRole && Number(this.profile.userRole.id) === 1); },
    locked() { return this.fields.some((field) => field.locked); },
  },
  methods: {
    load() { this.$api.getCustomFields().then((d) => { this.fields = d; }); },
    fieldTypeLabel(type) { return this.$t(`customFields.types.${type}`); },
    openForm(row) {
      this.editing = !!row; this.form = row ? { ...row, optionsText: (row.options || []).join('\n') } : {
        key: '', label: '', type: 'text', optionsText: '', description: '', required: false, active: true,
      }; this.modal = true;
    },
    save() {
      const d = {
        key: this.form.key,
        label: this.form.label,
        type: this.form.type,
        description: this.form.description,
        required: this.form.required,
        active: this.form.active,
        options: this.form.optionsText
          ? this.form.optionsText.split(/\r?\n|,/).map((v) => v.trim()).filter(Boolean) : [],
      };
      const p = this.editing ? this.$api.updateCustomField(this.form.key, d) : this.$api.createCustomField(d);
      p.then(() => { this.modal = false; this.load(); });
    },
    remove(row) { this.$utils.confirm(this.$t('customFields.deleteConfirm'), () => this.$api.deleteCustomField(row.key).then(() => this.load())); },
  },
  mounted() { this.load(); },
});
</script>

<style lang="scss" scoped>
.custom-fields {
  &__content {
    max-width: 1320px;
    margin: 0 auto;
    padding: 0 24px 40px;
  }

  .page-header {
    margin-bottom: 1.5rem;

    .title {
      line-height: 1.25;
      margin-bottom: .5rem !important;
    }

    // Bulma applies a negative margin to every title followed by a subtitle.
    // That is useful for compact headings, but makes this two-line intro
    // overlap at larger font sizes.
    .subtitle {
      margin-top: 0 !important;
    }
  }

  .subtitle {
    color: #697386;
    line-height: 1.65;
    max-width: 760px;
  }

  &__notice {
    display: flex;
    align-items: center;
    gap: .5rem;
    margin-bottom: 1.25rem;
  }

  &__table-card {
    min-height: 180px;
    padding: 0;
    overflow: hidden;
  }

  &__table :deep(.table-wrapper) {
    overflow-x: auto;
  }

  &__table :deep(th) {
    color: #4a5568;
    font-size: .8125rem;
    font-weight: 600;
    letter-spacing: .02em;
    white-space: nowrap;
  }

  &__table :deep(td),
  &__table :deep(th) {
    padding: 1rem 1.25rem;
    vertical-align: middle;
  }

  code {
    display: inline-block;
    border-radius: 4px;
    background: #f4f6f8;
    color: #52616b;
    font-size: .8125rem;
    padding: .3rem .55rem;
  }

  .placeholder-code {
    max-width: 100%;
    overflow: hidden;
    text-overflow: ellipsis;
    vertical-align: middle;
    white-space: nowrap;
  }

  .field-label,
  .field-actions {
    display: flex;
    align-items: center;
    gap: .5rem;
  }

  .field-actions {
    justify-content: center;
  }

  .field-actions :deep(.button) {
    min-width: 2.25rem;
  }
}

@media screen and (max-width: 768px) {
  .custom-fields__content {
    padding: 0 0 24px;
  }

  .custom-fields .page-header {
    margin-bottom: 1rem;
  }
}
</style>
