<script setup lang="ts">
import { computed } from 'vue';
import type { GrantSubjectType, UserGroupItem, UserItem } from '@/api/types';

const props = defineProps<{
  subjectType: GrantSubjectType;
  subjectId: number | undefined;
  users: UserItem[];
  groups: UserGroupItem[];
}>();

const emit = defineEmits<{
  'update:subjectType': [value: GrantSubjectType];
  'update:subjectId': [value: number | undefined];
}>();

const subjectLabel = computed(() => (props.subjectType === 'user' ? '用户' : '用户组'));
</script>

<template>
  <div class="step-block">
    <div class="step-title">① 主体(谁获得权限)</div>
    <el-form label-width="90px">
      <el-form-item label="主体类型">
        <el-radio-group
          :model-value="props.subjectType"
          @update:model-value="(v: GrantSubjectType) => emit('update:subjectType', v)"
        >
          <el-radio-button value="user">用户</el-radio-button>
          <el-radio-button value="group">用户组</el-radio-button>
        </el-radio-group>
      </el-form-item>
      <el-form-item :label="subjectLabel">
        <el-select
          :model-value="props.subjectId"
          :placeholder="`选择${subjectLabel}`"
          filterable
          style="width: 100%"
          @update:model-value="(v: number) => emit('update:subjectId', v)"
        >
          <template v-if="props.subjectType === 'user'">
            <el-option v-for="u in props.users" :key="u.id" :label="u.username" :value="u.id" />
          </template>
          <template v-else>
            <el-option v-for="g in props.groups" :key="g.id" :label="g.name" :value="g.id" />
          </template>
        </el-select>
      </el-form-item>
    </el-form>
  </div>
</template>

<style scoped>
.step-block {
  margin-bottom: 16px;
}
.step-title {
  font-size: 13px;
  font-weight: 600;
  color: var(--text-1);
  margin-bottom: 10px;
}
</style>
