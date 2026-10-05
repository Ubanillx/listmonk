<template>
  <div class="campaign-audience-tree" @keydown.esc.stop="close" @focusout="onFocusOut">
    <label class="label" for="campaign-audience-trigger">
      {{ label }} <span class="campaign-audience-count">({{ selectedItems.length }})</span>
    </label>

    <div class="campaign-audience-control">
      <button id="campaign-audience-trigger" ref="trigger" type="button" class="campaign-audience-trigger"
        :disabled="disabled" :aria-expanded="String(isOpen)" aria-haspopup="true"
        aria-controls="campaign-audience-options" data-cy="campaign-audience-trigger" @click="toggleOpen">
        <span class="campaign-audience-summary" :class="{ 'is-placeholder': selectedItems.length === 0 }">
          {{ summary }}
        </span>
        <b-icon :icon="isOpen ? 'chevron-up' : 'chevron-down'" size="is-small" />
      </button>

      <div v-if="isOpen" id="campaign-audience-options" class="campaign-audience-popup">
        <div class="campaign-audience-search">
          <input ref="search" v-model="query" class="input is-small" type="search"
            :placeholder="$t('campaigns.audienceSearch')" :aria-label="$t('campaigns.audienceSearch')" />
          <b-icon icon="magnify" size="is-small" />
        </div>

        <div class="campaign-audience-options">
          <template v-for="group in groups">
            <div v-if="group.items.length" :key="group.key" class="campaign-audience-group">
              <button type="button" class="campaign-audience-branch"
                :aria-expanded="String(expanded[group.key] || !!query)"
                @click="toggleGroup(group.key)">
                <b-icon :icon="expanded[group.key] || query ? 'chevron-down' : 'chevron-right'" size="is-small" />
                <span>{{ group.label }}</span>
                <span class="campaign-audience-group-count">{{ group.items.length }}</span>
              </button>
              <ul v-show="expanded[group.key] || query">
                <li v-for="list in group.items" :key="list.id">
                  <label class="campaign-audience-leaf" :class="{ 'is-disabled': optionDisabled(list) }">
                    <input type="checkbox" :checked="isSelected(list)" :disabled="optionDisabled(list)"
                      :data-cy="`campaign-audience-${list.id}`" @change="toggleList(list)" />
                    <span :title="list.name">{{ list.name }}</span>
                  </label>
                </li>
              </ul>
            </div>
          </template>
          <p v-if="!hasMatches" class="campaign-audience-empty">
            {{ query ? $t('campaigns.audienceNoResults') : $t('campaigns.audienceEmpty') }}
          </p>
        </div>
        <p v-if="exclusiveGroups || poolOnly" class="campaign-audience-help">
          {{ $t('campaigns.audienceExclusiveHelp') }}
        </p>
      </div>
    </div>

    <b-taglist v-if="selectedItems.length" class="campaign-audience-tags">
      <b-tag v-for="list in selectedItems" :key="list.id" :closable="!disabled" @close="removeList(list.id)">
        {{ list.name }} <span class="campaign-audience-tag-type">{{ groupLabel(list) }}</span>
      </b-tag>
    </b-taglist>
  </div>
</template>

<script>
export default {
  name: 'CampaignAudienceTreeSelect',

  props: {
    value: { type: Array, default: () => [] },
    all: { type: Array, default: () => [] },
    label: { type: String, default: '' },
    placeholder: { type: String, default: '' },
    disabled: Boolean,
    exclusiveGroups: Boolean,
    poolOnly: Boolean,
  },

  data() {
    return {
      isOpen: false,
      query: '',
      expanded: { private: true, pool: true },
    };
  },

  computed: {
    selectedItems() {
      return this.value || [];
    },
    summary() {
      if (!this.selectedItems.length) return this.placeholder;
      if (this.selectedItems.length === 1) return this.selectedItems[0].name;
      return this.$t('campaigns.audienceSelected', { count: this.selectedItems.length });
    },
    groups() {
      const term = this.query.trim().toLocaleLowerCase();
      const matches = (list) => !term || list.name.toLocaleLowerCase().includes(term);
      return [
        {
          key: 'private',
          label: this.$t('campaigns.privateAudienceLists'),
          items: this.all.filter((list) => list.type !== 'pool' && list.type !== 'org_pool_allocation' && matches(list)),
        },
        {
          key: 'pool',
          label: this.$t('campaigns.poolAudienceLists'),
          items: this.all.filter((list) => list.type === 'pool' && matches(list)),
        },
      ];
    },
    hasMatches() {
      return this.groups.some((group) => group.items.length > 0);
    },
  },

  methods: {
    groupKey(list) {
      return list.type === 'pool' ? 'pool' : 'private';
    },
    groupLabel(list) {
      return this.$t(this.groupKey(list) === 'pool'
        ? 'campaigns.poolAudienceLists' : 'campaigns.privateAudienceLists');
    },
    isSelected(list) {
      return this.selectedItems.some((item) => Number(item.id) === Number(list.id));
    },
    optionDisabled(list) {
      if (this.disabled) return true;
      if (this.isSelected(list)) return false;
      if (this.poolOnly && this.groupKey(list) !== 'pool') return true;
      return this.exclusiveGroups && this.selectedItems.length > 0
        && this.groupKey(list) !== this.groupKey(this.selectedItems[0]);
    },
    toggleList(list) {
      if (this.optionDisabled(list)) return;
      const selected = this.isSelected(list)
        ? this.selectedItems.filter((item) => Number(item.id) !== Number(list.id))
        : [...this.selectedItems, list];
      this.$emit('input', selected);
    },
    removeList(id) {
      if (this.disabled) return;
      this.$emit('input', this.selectedItems.filter((item) => Number(item.id) !== Number(id)));
    },
    toggleGroup(key) {
      if (this.query) return;
      this.$set(this.expanded, key, !this.expanded[key]);
    },
    toggleOpen() {
      this.isOpen = !this.isOpen;
      if (this.isOpen) {
        this.$nextTick(() => this.$refs.search.focus());
      } else {
        this.query = '';
      }
    },
    close() {
      if (!this.isOpen) return;
      this.isOpen = false;
      this.query = '';
      this.$refs.trigger.focus();
    },
    onFocusOut(event) {
      if (event.relatedTarget && !this.$el.contains(event.relatedTarget)) {
        this.isOpen = false;
        this.query = '';
      }
    },
    onDocumentClick(event) {
      if (!this.$el.contains(event.target)) {
        this.isOpen = false;
        this.query = '';
      }
    },
  },

  mounted() {
    document.addEventListener('click', this.onDocumentClick);
  },

  beforeDestroy() {
    document.removeEventListener('click', this.onDocumentClick);
  },
};
</script>

<style lang="scss" scoped>
.campaign-audience-tree {
  position: relative;
  width: 100%;
  max-width: var(--lm-field-max-width);
  margin-bottom: 1.5rem;
}

.campaign-audience-count {
  color: var(--lm-color-text-muted);
  font-weight: 400;
}

.campaign-audience-control {
  position: relative;
}

.campaign-audience-trigger {
  display: flex;
  align-items: center;
  justify-content: space-between;
  gap: 12px;
  width: 100%;
  min-height: 40px;
  padding: 8px 12px;
  border: 1px solid var(--lm-color-border-strong);
  border-radius: var(--lm-radius-md);
  background: var(--lm-color-surface);
  color: var(--lm-color-text);
  font: inherit;
  text-align: left;
  cursor: pointer;
}

.campaign-audience-trigger:focus-visible {
  border-color: var(--lm-color-primary);
  outline: 0;
  box-shadow: var(--lm-focus-ring);
}

.campaign-audience-trigger:disabled {
  cursor: not-allowed;
  opacity: 0.55;
}

.campaign-audience-summary {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.campaign-audience-summary.is-placeholder {
  color: var(--lm-color-text-muted);
}

.campaign-audience-popup {
  position: absolute;
  z-index: 40;
  top: calc(100% + 4px);
  left: 0;
  width: 100%;
  padding: 8px;
  border: 1px solid var(--lm-color-border);
  border-radius: var(--lm-radius-md);
  background: var(--lm-color-surface);
  box-shadow: var(--lm-shadow-md);
}

.campaign-audience-search {
  position: relative;
  margin-bottom: 6px;
}

.campaign-audience-search .input {
  width: 100%;
  padding-right: 34px;
}

.campaign-audience-search .icon {
  position: absolute;
  top: 50%;
  right: 8px;
  transform: translateY(-50%);
  pointer-events: none;
  color: var(--lm-color-text-muted);
}

.campaign-audience-options {
  max-height: 300px;
  overflow-y: auto;
}

.campaign-audience-group ul {
  margin: 0;
  padding: 0;
  list-style: none;
}

.campaign-audience-branch {
  display: flex;
  align-items: center;
  gap: 6px;
  width: 100%;
  min-height: 36px;
  padding: 6px 10px;
  border: 0;
  border-radius: var(--lm-radius-sm);
  background: transparent;
  color: var(--lm-color-text);
  font: inherit;
  font-weight: 600;
  text-align: left;
  cursor: pointer;
}

.campaign-audience-branch:hover,
.campaign-audience-leaf:hover {
  background: var(--lm-color-surface-subtle);
}

.campaign-audience-group-count {
  margin-left: auto;
  color: var(--lm-color-text-muted);
  font-size: 0.75rem;
  font-weight: 400;
}

.campaign-audience-leaf {
  display: flex;
  align-items: center;
  gap: 10px;
  min-height: 36px;
  padding: 6px 10px 6px 36px;
  border-radius: var(--lm-radius-sm);
  cursor: pointer;
}

.campaign-audience-leaf input {
  flex: 0 0 auto;
  accent-color: var(--lm-color-primary);
}

.campaign-audience-leaf span {
  min-width: 0;
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.campaign-audience-leaf.is-disabled {
  cursor: not-allowed;
  opacity: 0.5;
}

.campaign-audience-empty,
.campaign-audience-help {
  padding: 8px 10px;
  color: var(--lm-color-text-muted);
  font-size: 0.8125rem;
}

.campaign-audience-help {
  margin-top: 6px;
  border-top: 1px solid var(--lm-color-border);
}

.campaign-audience-tags {
  margin-top: 8px;
}

.campaign-audience-tag-type {
  margin-left: 4px;
  color: var(--lm-color-text-muted);
  font-weight: 400;
}
</style>
